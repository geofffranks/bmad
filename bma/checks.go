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
	Bulk         bool                 // Is this check a bulk-mode check
	Report       bool                 // Should this check report its exit code as a STATE event? (bulk-mode only)
	Name         string               // Name of the Check

	cmd_args     []string
	process     *exec.Cmd
	rc           int
	attempts     int
	stdout      *bytes.Buffer
	stderr      *bytes.Buffer

	started_at   time.Time
	ended_at     time.Time
	next_run     time.Time
	latency      int64
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
		process.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}}
	}

	if err := process.Start(); err != nil {
		return err
	}
	log.Debug("Spawned check %s[%d]", self.Name, process.Process.Pid)

	self.started_at  = time.Now()
	self.ended_at    = time.Time{}
	self.running     = true
	self.duration    = 0
	self.process     = process
	self.sig_term    = false
	self.sig_kill    = false
	self.stdout      = &o
	self.stderr      = &e

	return nil
}

// Called on running checks, to determine if they have finished
// running.
//
// If the Check has not finished executing, returns an empty
// string, and false. If not, returns an empty string, and false.
// If the Check has been running for longer than its Timeout,
// a SIGTERM (and failing that a SIGKILL) is issued to forcibly
// terminate the rogue Check process. In either case, this returns
// as if the check has not yet finished, and Reap() will need to be
// called again to fully reap the Check
//
// If the Check has finished execution (on its own, or via forced
// termination), it will return the check output, and true.
//
// Once complete, some additional meta-stats for the check execution
// are appended to the check output, to be submit up to bolo
func (self *Check) Reap() (string, bool) {
	pid := self.process.Process.Pid

	//FIXME:  verify large buffer output doesn't cause deadlocking
	var ws syscall.WaitStatus
	status, err := syscall.Wait4(pid, &ws, syscall.WNOHANG, nil);
	if err != nil {
		log.Error("Error waiting on check %s[%d]: %s", self.Name, pid, err.Error())
		return "", false
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
		if time.Now().After(self.started_at.Add(time.Duration(self.Timeout) * time.Second)) {
			log.Warn("Check %s[%d] has been running too long, sending SIGTERM", self.Name, pid)
			if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
				log.Error("Error sending SIGTERM to check %s[%d]: %s", self.Name, pid, err.Error())
			}
			self.sig_term = true
		}
		return "", false
	}

	self.ended_at = time.Now()
	self.running  = false
	self.duration = time.Since(self.started_at)
	self.latency  = self.started_at.UnixNano() - self.next_run.UnixNano()
	output       := string(self.stdout.Bytes())
	err_msg      := string(self.stderr.Bytes())

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
	if (! self.Bulk && self.rc != OK && self.attempts < self.Retries) {
		self.schedule(self.started_at, self.Retry_every)
		self.attempts++
	} else {
		if (self.rc == OK) {
			self.attempts = 0
		}
		self.schedule(self.started_at, self.Every)
	}
	if self.ended_at.After(self.next_run) {
		timeout_triggered := "not reached"
		if self.sig_term || self.sig_kill {
			timeout_triggered = "reached"
		}
		log.Warn("Check %s[%d] took %0.3f seconds to run, at interval %d (timeout of %d was %s)",
			self.Name, pid, self.duration.Seconds(), self.Every, self.Timeout, timeout_triggered)
	}

	// Add meta-stats for bmad
	var msg string
	if (self.Bulk && self.Report) {
		// check-specific state (for bulk data-submitter checks)
		if self.rc == OK {
			msg = self.Name + " completed successfully!"
		} else {
			msg = strings.Replace(err_msg, "\n", " ", -1)
		}
		output = output + fmt.Sprintf("STATE %d %s:bmad:%s %d %s\n",
			time.Now().Unix(), cfg.Host, self.Name, self.rc, msg)
	}
	// check-specific runtime
	output = output + fmt.Sprintf("SAMPLE %d %s:bmad:%s:exec-time %0.4f\n",
		time.Now().Unix(), cfg.Host, self.Name, self.duration.Seconds())
	// bmad overall check throughput measurement
	output = output + fmt.Sprintf("COUNTER %d %s:bmad:checks\n",
		time.Now().Unix(), cfg.Host)
	// bmad avg check runtime
	output = output + fmt.Sprintf("SAMPLE %d %s:bmad:exec-time %d\n",
		time.Now().Unix(), cfg.Host, self.duration)
	// bmad avg check latency
	output = output + fmt.Sprintf("SAMPLE %d %s:bmad:latency %d\n",
		time.Now().Unix(), cfg.Host, self.latency)

	return output, true
}

// Determines whether or not a Check should be run
func (self *Check) ShouldRun() (bool) {
	return ! self.running && time.Now().After(self.next_run)
}
