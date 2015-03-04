package bma

import "bytes"
import "fmt"
import "os/exec"
import "os/user"
import "strings"
import "strconv"
import "syscall"
import "time"

type Check struct {
	// Command to execute for this Check
	Command      string
	// Specific interval at which to run this Check (in seconds)
	Every        int64
	// Number of times to retry this Check after failure
	Retries      int
	// Retry interval at which to retry after Check failure (in secons)
	Retry_every  int64
	// Maximum execution time for the Check (in seconds)
	Timeout      int64
	// Map of environment variables to set during Check execution
	Env          map[string]string
	// User name to run this Check as
	Run_as       string
	// Determines whether this Check is running in bulk-mode
	// (multiple datapoints are being submitted). Non-bulk mode
	// is usually used for individual STATE checks.
	Bulk         bool
	// Determines whether or not this Check will have its execution
	// return code be auto-submitted as a STATE message for the
	// execution of the Check. This is most useful (and only allowed)
	// for bulk-mode Checks
	Report       bool
	// Name of the Check
	Name         string

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
	self.schedule(time.Now(), self.Every)

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
		log.Debug("Check requested to run as %q", self.Run_as)
		process.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}}
	}

	if err := process.Start(); err != nil {
		return err
	}
	log.Debug("Check %q initiated as process %d", self.Name, process.Process.Pid)

	self.started_at  = time.Now()
	self.ended_at    = time.Time{}
	self.duration   = 0
	self.process    = process
	self.sig_term    = false
	self.sig_kill    = false
	self.stdout     = &o
	self.stderr     = &e

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
		log.Error("Error waiting on process %d: %s", pid, err.Error())
	}
	if status == 0 {
		// self to see if we need to sigkill due to failed sigterm
		if self.started_at.After(time.Now().Add(time.Duration(self.Timeout + 2) * time.Second)) {
			if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
				log.Error("Error sending SIGKILL to process %d: %s", pid, err.Error())
			}
			self.sig_kill = true
		}
		// self to see if we need to sigterm due to self timeout expiry
		if self.started_at.After(time.Now().Add(time.Duration(self.Timeout) * time.Second)) {
			if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
				log.Error("Error sending SIGTERM to process %d: %s", pid, err.Error())
			}
			self.sig_term = true
		}
		return "", false
	}

	self.ended_at = time.Now()
	self.duration = time.Since(self.started_at)
	self.latency  = self.started_at.UnixNano() - self.next_run.UnixNano()
	output       := string(self.stdout.Bytes())
	err_msg      := string(self.stderr.Bytes())

	if (ws.Exited()) {
		self.rc = ws.ExitStatus()
	} else {
		log.Debug("%s exited abnormally (signaled/stopped). Setting rc to UNKNOWN")
		self.rc = UNKNOWN
	}
	if self.rc > UNKNOWN {
		log.Debug("%s returned with an invalid exit code. Setting rc to UNKOWN")
		self.rc = UNKNOWN
	}
	if (! self.Bulk && self.rc != OK && self.attempts < self.Retries) {
		self.schedule(self.started_at, self.Retry_every)
		self.attempts++
	} else {
		if (self.rc == OK) {
			self.attempts = 0
		}
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
	output = output + fmt.Sprintf("SAMPLE %d %s:bmad:%s:exec-time %d\n",
		time.Now().Unix(), cfg.Host, self.Name, self.duration)
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
	return time.Now().After(self.next_run)
}
