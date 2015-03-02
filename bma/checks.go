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
	Command      string
	Interval     int64
	Retry        int64
	Timeout      int64
	Max_attempts int
	Env          map[string]string
	Run_as       string
	Bulk         bool
	Report_state bool
	Name         string

	cmd_args     []string
	process     *exec.Cmd
	rc           int
	attempts     int
	stdout      *bytes.Buffer
	stderr      *bytes.Buffer

	started_at    time.Time
	ended_at      time.Time
	next_run      time.Time
	latency      int64
	duration     time.Duration

	sig_term      bool
	sig_kill      bool
}

const OK       int = 0
const WARNING  int = 1
const CRITICAL int = 2
const UNKNOWN  int = 3

func (self *Check) environment() ([]string) {
	var env []string
	for k, v := range(self.Env) {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	return env
}

func (self *Check) schedule(last_started time.Time, interval int64) () {
	if (interval <= 0) {
		interval = self.Interval
	}
	self.next_run = last_started.Add(time.Duration(interval) * time.Second)
}

func (self *Check) Spawn() (error) {
	self.schedule(time.Now(), self.Interval)

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
	if (! self.Bulk && self.rc != OK && self.attempts < self.Max_attempts) {
		self.schedule(self.started_at, self.Retry)
		self.attempts++
	} else {
		if (self.rc == OK) {
			self.attempts = 0
		}
	}

	// Add meta-stats for bmad
	var msg string
	if (self.Bulk && self.Report_state) {
		// check-specific state (for bulk data-submitter checks)
		if self.rc == OK {
			msg = self.Name + " completed successfully!"
		} else {
			msg = strings.Replace(err_msg, "\n", " ", -1)
		}
		output = output + fmt.Sprintf("STATE %d %s:bmad:%s %d %s\n",
			time.Now().Unix(), cfg.host, self.Name, self.rc, msg)
	}
	// check-specific runtime
	output = output + fmt.Sprintf("SAMPLE %d %s:bmad:%s:exec-time %d\n",
		time.Now().Unix(), cfg.host, self.Name, self.duration)
	// bmad overall check throughput measurement
	output = output + fmt.Sprintf("COUNTER %d %s:bmad:checks\n",
		time.Now().Unix(), cfg.host)
	// bmad avg check runtime
	output = output + fmt.Sprintf("SAMPLE %d %s:bmad:exec-time %d\n",
		time.Now().Unix(), cfg.host, self.duration)
	// bmad avg check latency
	output = output + fmt.Sprintf("SAMPLE %d %s:bmad:latency %d\n",
		time.Now().Unix(), cfg.host, self.latency)

	return output, true
}

func (self *Check) ShouldRun() (bool) {
	return time.Now().After(self.next_run)
}
