package bma

import "github.com/geofffranks/bmad/log"
import "bytes"
import "errors"
import "fmt"
import "os/exec"
import "os/user"
import "strings"
import "strconv"
import "syscall"
import "time"

// Checks in bmad come in two flavors - bulk, and regular.
// Both modes represent commands that should be run at specific
// intervals, whose output get piped up to a Bolo server.
//
// Bulk mode Checks differ from Non-Bulk mode Checks in that
// they are expected to submit multiple datapoints/or states
// for a single execution (e.g. the sar collector, reporting
// all sar metrics with a single execution). Bulk checks are
// for this reason also allowed to submit meta-checks about
// their execution state to bolo, based on their return code.
//
// The primary use case for non-bulk checks is to report a single
// metric, or state up to bolo, and thus state meta-checks are
// disallowed.
type Check struct {
	Command      string               // Command to execute for this Check
	Every        int64                // Specific interval at which to run this Check (in seconds)
	Retries      int                  // Number of times to retry this Check after failure
	Retry_every  int64                // Retry interval at which to retry after Check failure (in secons)
	Timeout      int64                // Maximum execution time for the Check (in seconds)
	Env          map[string]string    // Map of environment variables to set during Check execution
	Run_as       string               // User name to run this Check as
	Bulk         string               // Is this check a bulk-mode check
	Report       string               // Should this check report its exit code as a STATE event? (bulk-mode only)
	Name         string               // Name of the Check

	cmd_args     []string
	process     *exec.Cmd
	rc           int
	attempts     int
	stdout      *bytes.Buffer
	stderr      *bytes.Buffer
	output       string
	err_msg      string

	started_at   time.Time
	ended_at     time.Time
	next_run     time.Time
	latency      time.Duration
	duration     time.Duration
	running      bool

	sig_term     bool
	sig_kill     bool
}

const OK       int = 0
const WARNING  int = 1
const CRITICAL int = 2
const UNKNOWN  int = 3

// Converts the Check's environment variable map
// into an array of bash-compatibally formated environment
// variables.
func (self *Check) environment() ([]string) {
	var env []string
	for k, v := range(self.Env) {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	return env
}
// Merges relavent data from an old check into the new check
// so that upon config reload, we can retain state properly
func merge_checks(check *Check, old *Check) {
	check.next_run   = old.next_run
	check.started_at = old.started_at
	check.ended_at   = old.ended_at
	check.duration   = old.duration
	check.attempts   = old.attempts
	check.rc         = old.rc
	check.latency    = old.latency
	check.stdout     = old.stdout
	check.stderr     = old.stderr
	check.output     = old.output
	check.err_msg    = old.err_msg
	check.sig_term   = old.sig_term
	check.sig_kill   = old.sig_kill
	check.process    = old.process
	check.running    = old.running
}

// Schedules the next run of the Check. If interval is
// not provided, defaults to the Every value of the Check.
func (self *Check) schedule(last_started time.Time, interval int64) () {
	if (interval <= 0) {
		interval = self.Every
	}
	self.next_run = last_started.Add(time.Duration(interval) * time.Second)
}

// Does the needful to kick off a check. This will set the
// environment variables, pwd, effective user/group, hook
// up buffers for grabbing check output, run the process,
// and fill out accounting data for the check.
func (self *Check) Spawn() (error) {
	if self.running {
		return errors.New(fmt.Sprintf("check %s[%d] is already running", self.Name, self.process.Process.Pid))
	}

	process := exec.Command(self.cmd_args[0], self.cmd_args[1:]...)
	process.Env = self.environment()
	process.Dir = "/"
	var o bytes.Buffer
	var e bytes.Buffer
	process.Stdout = &o
	process.Stderr = &e
	self.output  = ""
	self.err_msg = ""

	// Reset started_at as soon as possible after determining there isn't
	// a check already running. This way, if there are errors we can
	// back-off rescheduling, rather than try every tick, for relatively
	// long-term fixes (user creation, file creation/renames/permissions)
	self.started_at  = time.Now()

	if self.Run_as != "" {
		u, err := user.Lookup(self.Run_as)
		if err != nil {
			return err
		}
		uid, err := strconv.ParseUint(u.Uid, 10, 32)
		if err != nil {
			return err
		}
		gid, err := strconv.ParseUint(u.Gid, 10, 32)
		if err != nil {
			return err
		}
		log.Debug("Running check %s as %q", self.Name, self.Run_as)
		process.SysProcAttr = &syscall.SysProcAttr{
			Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)},
		}
	}

	if err := process.Start(); err != nil {
		return err
	}
	log.Debug("Spawned check %s[%d]", self.Name, process.Process.Pid)

	self.running     = true
	self.process     = process
	self.stdout      = &o
	self.stderr      = &e
	self.sig_term    = false
	self.sig_kill    = false
	self.ended_at    = time.Time{}
	self.duration    = 0

	return nil
}

// Called on running checks, to determine if they have finished
// running.
//
// If the Check has not finished executing, returns false.
//
// If the Check has been running for longer than its Timeout,
// a SIGTERM (and failing that a SIGKILL) is issued to forcibly
// terminate the rogue Check process. In either case, this returns
// as if the check has not yet finished, and Reap() will need to be
// called again to fully reap the Check
//
// If the Check has finished execution (on its own, or via forced
// termination), it will return true.
//
// Once complete, some additional meta-stats for the check execution
// are appended to the check output, to be submit up to bolo
func (self *Check) Reap() (bool) {
	pid := self.process.Process.Pid

	var ws syscall.WaitStatus
	status, err := syscall.Wait4(pid, &ws, syscall.WNOHANG, nil);
	if err != nil {
		log.Error("Error waiting on check %s[%d]: %s", self.Name, pid, err.Error())
		return false
	}
	if status == 0 {
		// self to see if we need to sigkill due to failed sigterm
		if time.Now().After(self.started_at.Add(time.Duration(self.Timeout + 2) * time.Second)) {
			log.Warn("Check %s[%d] has been running too long, sending SIGKILL", self.Name, pid)
			if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
				log.Error("Error sending SIGKILL to check %s[%d]: %s", self.Name, pid, err.Error())
			}
			self.sig_kill = true
		}
		// self to see if we need to sigterm due to self timeout expiry
		if ! self.sig_kill && time.Now().After(self.started_at.Add(time.Duration(self.Timeout) * time.Second)) {
			log.Warn("Check %s[%d] has been running too long, sending SIGTERM", self.Name, pid)
			if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
				log.Error("Error sending SIGTERM to check %s[%d]: %s", self.Name, pid, err.Error())
			}
			self.sig_term = true
		}
		return false
	}

	self.ended_at = time.Now()
	self.running  = false
	self.duration = time.Since(self.started_at)
	self.latency  = self.started_at.Sub(self.next_run)
	self.output   = string(self.stdout.Bytes())
	self.err_msg  = string(self.stderr.Bytes())

	if (ws.Exited()) {
		self.rc = ws.ExitStatus()
	} else {
		log.Debug("Check %s[%d] exited abnormally (signaled/stopped). Setting rc to UNKNOWN", self.Name, pid)
		self.rc = UNKNOWN
	}
	if self.rc > UNKNOWN {
		log.Debug("Check %s[%d] returned with an invalid exit code. Setting rc to UNKOWN", self.Name, pid)
		self.rc = UNKNOWN
	}

	self.reschedule()

	if self.ended_at.After(self.next_run) {
		timeout_triggered := "not reached"
		if self.sig_term || self.sig_kill {
			timeout_triggered = "reached"
		}
		log.Warn("Check %s[%d] took %0.3f seconds to run, at interval %d (timeout of %d was %s)",
			self.Name, pid, self.duration.Seconds(), self.Every, self.Timeout, timeout_triggered)
	}
	return true
}

// Submits check results to bolo. This will append meta-stats
// to the checks as well, for bmad (like checks run, execution
// time, check latency). If the check has Bulk and Report both set
// to "true", it will report a STATE for the bulk check's execution.
// If the bulk check failed, any output to stderr will be included
// in the status message.
//
// If full_stats is set to false, the latency, and count of checks run
// will *NOT* be reported. This is primarily used internally
// for reporting stats differently for run-once mode vs daemonized.
func (self *Check) Submit(full_stats bool) (error) {
	// Add meta-stats for bmad
	var meta string
	var msg string
	if (self.Bulk == "true" && self.Report == "true") {
		// check-specific state (for bulk data-submitter checks)
		if self.rc == OK {
			msg = self.Name + " completed successfully!"
		} else {
			msg = strings.Replace(self.err_msg, "\n", " ", -1)
		}
		meta = fmt.Sprintf("STATE %d %s:bmad:%s %d %s",
			time.Now().Unix(), cfg.Host, self.Name, self.rc, msg)
	}
	// check-specific runtime
	meta = fmt.Sprintf("%s\nSAMPLE %d %s:bmad:%s:exec-time %0.4f",
		meta, time.Now().Unix(), cfg.Host, self.Name, self.duration.Seconds())
	// bmad avg check runtime
	meta = fmt.Sprintf("%s\nSAMPLE %d %s:bmad:exec-time %0.4f",
		meta, time.Now().Unix(), cfg.Host, self.duration.Seconds())

	if full_stats {
		// bmad avg check latency
		meta = fmt.Sprintf("%s\nSAMPLE %d %s:bmad:latency %0.4f",
			meta, time.Now().Unix(), cfg.Host, self.latency.Seconds())
		// bmad overall check throughput measurement
		meta = fmt.Sprintf("%s\nCOUNTER %d %s:bmad:checks",
			meta, time.Now().Unix(), cfg.Host)
	}

	meta = meta + "\n"
	log.Debug("%s output: %s", self.Name, self.output)
	var err error
	if self.Bulk == "true" || self.attempts >= self.Retries {
		err = SendToBolo(fmt.Sprintf("%s\n%s", self.output, meta))
	} else {
		log.Debug("%s not yet at max attempts, suppressing output submission", self.Name)
		err = SendToBolo(meta)
	}
	if err != nil {
		return err
	}
	return nil
}

func (self *Check) Fail(failure error) (error) {
	log.Error("Error running check \"%s\": %s", self.Name, failure.Error())
	var err error
	self.rc = 3
	self.reschedule()
	if (self.Report == "true") {
		if (self.Bulk == "true" || self.attempts >= self.Retries) {
			msg := fmt.Sprintf("STATE %d %s:bmad:%s %d %s",
				time.Now().Unix(), cfg.Host, self.Name, self.rc, "failed to exec: " + failure.Error())
			err = SendToBolo(msg)
		}
	}
	return err
}

// Determines whether or not a Check should be run
func (self *Check) ShouldRun() (bool) {
	return ! self.running && time.Now().After(self.next_run)
}

// Returns the last output of a check
func (self *Check) Output() (string) {
	return self.output
}

func (self *Check) reschedule() {
	self.schedule(self.started_at, self.Every)
	if self.Bulk != "true" {
		if self.rc != OK {
			self.attempts++
			if self.attempts < self.Retries {
				self.schedule(self.started_at, self.Retry_every)
			}
		} else {
			self.attempts = 0
		}
	}
}
