package bma

import "testing"
import "github.com/stretchr/testify/assert"
import "bytes"
import "fmt"
import "os"
import "os/exec"
import "os/user"
import "regexp"
import "strings"
import "time"


func Test_merge_checks(t *testing.T) {
	old := Check{
		Command:       "echo \"third success\"",
		Every:         30,
		Retry_every:   25,
		Retries:       10,
		Timeout:       20,
		Env:           map[string]string{},
		Name:          "third",
		cmd_args:      []string{"echo", "third success"},
		next_run:      time.Unix(42,0),
		started_at:    time.Unix(15,0),
		ended_at:      time.Unix(20,0),
		duration:      5,
		attempts:      2,
		rc:            2,
		latency:       1345,
		stdout:        &bytes.Buffer{},
		stderr:        &bytes.Buffer{},
		sig_term:      true,
		sig_kill:      true,
		process:       &exec.Cmd{},
		running:       true,
	}
	c := Check{
		Command:     "echo \"new command\"",
		Every:       40,
		Retry_every: 27,
		Retries:     15,
		Timeout:     22,
		Env:         map[string]string{"env": "value"},
		Name:        "third",
		cmd_args:    []string{"echo","new command"},
	}
	expect := Check{
		Command:       "echo \"new command\"",
		Every:         40,
		Retry_every:   27,
		Retries:       15,
		Timeout:       22,
		Env:           map[string]string{"env": "value"},
		Name:          "third",
		cmd_args:      []string{"echo", "new command"},
		next_run:      time.Unix(42,0),
		started_at:    time.Unix(15,0),
		ended_at:      time.Unix(20,0),
		duration:      5,
		attempts:      2,
		rc:            2,
		latency:       1345,
		stdout:        old.stdout,
		stderr:        old.stderr,
		sig_term:      true,
		sig_kill:      true,
		process:       old.process,
		running:       true,
	}

	merge_checks(&c, &old)
	assert.Equal(t, expect, c, "merge_checks() merges all relevant data from old into new")
}

func Test_ShouldRun(t *testing.T) {
	check := Check{}

	assert.Equal(t, true, check.ShouldRun(), "check should run when running is false and next run is before now")

	check.running = true
	assert.Equal(t, false, check.ShouldRun(), "check shouldn't run if running already")

	check.running = false
	check.next_run = time.Now().Add(1 * time.Hour)
	assert.Equal(t, false, check.ShouldRun(), "check shouldn't run if next run is in the future")

	check.next_run = time.Now().Add(-1 * time.Hour)
	assert.Equal(t, true, check.ShouldRun(), "check should run when next_run exists and is in the past")
}

func Test_environment(t *testing.T) {
	check := Check{
		Env: map[string]string{
			"env1": "val1",
			"env2": "val2",
		},
	}
	expect := []string{"env1=val1", "env2=val2"}
	assert.Equal(t, expect, check.environment(), "Env gets translated into array of env vars correctly")
}

func Test_schedule(t *testing.T) {
	check := Check{
		Every:     300,
	}
	check.schedule(time.Unix(42,0), 0)
	assert.Equal(t, time.Unix(342, 0), check.next_run, "scheduling a check without an interval uses Every")

	check.schedule(time.Unix(42,0), 60)
	assert.Equal(t, time.Unix(102,0), check.next_run, "scheduling a check with an interval uses that interval")
}

func (check *Check) test(t *testing.T, expect_out string, expect_rc int, message string) {
	if message == "" {
		message = "test_check"
	}
	os.Chmod(check.cmd_args[0], 0755)
	err := check.Spawn()
	if ! assert.True(t, check.running, "%s: check.running is true post-spawn", message) {
		t.Fatalf("Couldn't spawn check, bailing out: %s", err.Error())
	}
	assert.NoError(t, err, "%s: no errors on successful spawning of check", message)
	assert.NotNil(t, check.process, "%s: check has a process", message)
	assert.WithinDuration(t, time.Now(), check.started_at, 1 * time.Second,
		"%s check started within 1 second from now", message)
	assert.Equal(t, check.ended_at, time.Time{}, "%s: check has no end time (yet)", message)
	assert.False(t, check.sig_term, "%s: check has not been sigtermed", message)
	assert.False(t, check.sig_kill, "%s: check has not been sigkilled", message)

	var finished bool
	for i := 0; i < 100; i++ {
		finished = check.Reap()
		if (finished) {
			break;
		}
		time.Sleep(100 * time.Millisecond)
		assert.Equal(t, "", check.output, "%s: check has no output yet")
	}
	assert.True(t,  finished, "%s: check finished", message)
	assert.Equal(t, expect_out, check.output, "%s: check output was as expected", message)
	assert.Equal(t, expect_rc,  check.rc,     "%s: check rc was as expected", message)
	assert.False(t, check.running, "%s: check is no longer running", message)

}

func Test_large_output(t *testing.T) {
	pwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Couldn't get working directory of tests: %s", err.Error())
	}
	cfg = &Config{
		Host: "test01.example.com",
	}
	check := Check{
		cmd_args: []string{pwd + "/t/bin/test_large_output"},
		Env:      map[string]string{"VAR1": "is set"},
		Name:     "test_large_output",
		Every:    300,
		Timeout:  20,
	}

	expect_out := strings.Repeat(strings.Repeat(".", 8193) + "done\n", 10)
	check.test(t, expect_out, 0, "large output succeeds without deadlocking")
}

func Test_check_lifecycle(t *testing.T) {
	pwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Couldn't get working directory of tests: %s", err.Error())
	}
	whoami, err := user.Current()
	if err != nil {
		t.Fatalf("Couldn't find current user. Bailing out: %s", err.Error())
	}
	cfg = &Config{
		Host: "test01.example.com",
	}
	check := Check{
		cmd_args: []string{"t/bin/not_a_check"},
		Env:      map[string]string{"VAR1": "is set"},
		Name:     "test_check",
		Every:    300,
		Timeout:  1,
		running:  true,
		process:  &exec.Cmd{
			Process: &os.Process{
				Pid: 1234567,
			},
		},
	}

	test_check := pwd + "/t/bin/test_check"
	expect_out := fmt.Sprintf("VAR1 is set\nRunning as '%s'\n", whoami.Username)

	err = check.Spawn()
	assert.EqualError(t, err, "check test_check[1234567] is already running",
		"check.Spawn() fails if already running")

	finished := check.Reap()
	assert.False(t, finished, "Reap() on an inactive check returns false")

	check.running = false
	check.process = nil
	err = check.Spawn()
	assert.EqualError(t, err, "exec: \"t/bin/not_a_check\": stat t/bin/not_a_check: no such file or directory",
		"check.Spawn() fails on bad command")

	check.cmd_args = []string{test_check, "hang", "0"}
	check.test(t, expect_out, 3, "hanging check")

	assert.True(t, check.sig_term, "check was sigtermed")
	assert.True(t, check.sig_kill, "check was sigkilled")
	assert.WithinDuration(t, time.Now(), check.ended_at, 1 * time.Second,
		"check ended within 1 second from now")
	assert.InDelta(t, 3.5 * float64(time.Second), float64(check.duration), 0.6 * float64(time.Second),
		"check duration is between 3 and 4 seconds")
	assert.Equal(t, check.started_at.Sub(time.Time{}), check.latency, "check latency checks out")
	assert.Equal(t, check.started_at.Add(time.Duration(check.Every) * time.Second), check.next_run,
		"next run for check is scheduled")

	check.cmd_args = []string{test_check, "2"}
	check.test(t, expect_out, 2, "CRITICAL check")

	check.cmd_args = []string{test_check, "0"}
	check.test(t, expect_out, 0, "OK check")

	check.cmd_args = []string{test_check, "15"}
	check.test(t, expect_out, 3, "bad exit code")

	// Testing attempts/retries logic
	check.cmd_args = []string{test_check, "2"}
	check.Retries     = 3
	check.Retry_every = 60
	check.attempts    = 0

	check.test(t, expect_out, 2, "1st attempt failure")
	assert.Equal(t, 1, check.attempts, "check is now 1st attempt failure")
	assert.Equal(t, check.started_at.Add(time.Duration(check.Retry_every) * time.Second), check.next_run,
		"next_run for 1st attempt uses Retry interval")

	check.test(t, expect_out, 2, "2nd attempt failure")
	assert.Equal(t, 2, check.attempts, "check is now 2nd attempt failure")
	assert.Equal(t, check.started_at.Add(time.Duration(check.Retry_every) * time.Second), check.next_run,
		"next_run for 2nd attempt uses Retry interval")

	check.test(t, expect_out, 2, "3rd attempt failure")
	assert.Equal(t, 3, check.attempts, "check is now 3rd attempt failure")
	assert.Equal(t, check.started_at.Add(time.Duration(check.Every) * time.Second), check.next_run,
		"next_run for 3rd attempt uses normal interval")

	check.test(t, expect_out, 2, "4th attempt failure")
	assert.Equal(t, 4, check.attempts, "check is now 4th attempt failure")
	assert.Equal(t, check.started_at.Add(time.Duration(check.Every) * time.Second), check.next_run,
		"next_run for 4th attempt uses normal interval")

	check.cmd_args = []string{test_check, "0"}
	check.test(t, expect_out, 0, "5th attempt recovery")
	assert.Equal(t, 0, check.attempts, "check is rest to 0 attempts on recovery")
	assert.Equal(t, check.started_at.Add(time.Duration(check.Every) * time.Second), check.next_run,
		"next_run uses normal interval after recovery")

	check.cmd_args = []string{test_check, "2"}

	check.test(t, expect_out, 2, "1st attempt failure")
	assert.Equal(t, 1, check.attempts, "check is now 1st attempt failure")
	assert.Equal(t, check.started_at.Add(time.Duration(check.Retry_every) * time.Second), check.next_run,
		"next_run for 1st attempt uses Retry interval")

	check.cmd_args = []string{test_check, "0"}
	check.test(t, expect_out, 0, "recovery before max attempts")
	assert.Equal(t, 0, check.attempts,
		"check is rest to 0 attempts on recovery, despite not hitting max attempts")
	assert.Equal(t, check.started_at.Add(time.Duration(check.Every) * time.Second), check.next_run,
		"next_run uses normal interval after recovery, depite not hitting max attempts")

	// Run_as testing
	if (whoami.Uid == "0") {
		check.Run_as = "nobody"
		check.test(t, "VAR1 is set\nRunning as 'nobody'\n", 0, "run_as nobody")
	}
}

func Test_Submit(t *testing.T) {
	check := Check{
		Name:     "test_check",
		Bulk:     "true",
		Report:   "true",
		output:   "myoutput\n",
		err_msg:  "myerror\nsecondline",
		rc:       0,
		duration: time.Duration(42 * time.Second),
		latency:  time.Duration(24 * time.Millisecond),
	}
	cfg = &Config{
		Host:            "test01.example.com",
	}

	output := check.test_submission(t, false, 1024)
	expect := regexp.MustCompile(fmt.Sprintf("STATE \\d+ test01.example.com:bmad:test_check 0 %s",
		"test_check completed successfully!"))
	assert.Regexp(t, expect, output, "bulk + report returns state")

	check.rc = 2
	output = check.test_submission(t, false, 1024)
	expect = regexp.MustCompile(fmt.Sprintf("STATE \\d+ test01.example.com:bmad:test_check 2 %s",
		"myerror secondline"))
	assert.Regexp(t, expect, output, "bulk + report with non-ok state gets stderr")

	check.Report = "false"; check.Bulk = "true"
	output = check.test_submission(t, false, 1024)
	expect = regexp.MustCompile("STATE")
	assert.NotRegexp(t, expect, output, "bulk + noreport doesn't do state")

	check.Report = "true"; check.Bulk = "false"
	output = check.test_submission(t, false, 1024)
	assert.NotRegexp(t, expect, output, "nobulk + report doesn't do state")

	output = check.test_submission(t, true, 1024)
	expect = regexp.MustCompile("^myoutput")
	assert.Regexp(t, expect, output, "normal check output is still present")
	expect = regexp.MustCompile("COUNTER \\d+ test01.example.com:bmad:checks")
	assert.Regexp(t, expect, output, "bmad check counter meta-stat is reported")
	expect = regexp.MustCompile("SAMPLE \\d+ test01.example.com:bmad:latency 0.0240")
	assert.Regexp(t, expect, output, "bmad latency meta-stat is reported")
	expect = regexp.MustCompile("SAMPLE \\d+ test01.example.com:bmad:exec-time 42.0000")
	assert.Regexp(t, expect, output, "bmad exec time meta-stat is reported")
	expect = regexp.MustCompile("SAMPLE \\d+ test01.example.com:bmad:test_check:exec-time 42.000")
	assert.Regexp(t, expect, output, "bmad check exec time meta-stat is reported")

	output = check.test_submission(t, false, 1024)
	expect = regexp.MustCompile("^myoutput")
	assert.Regexp(t, expect, output, "normal check output is still present")
	expect = regexp.MustCompile("SAMPLE \\d+ test01.example.com:bmad:exec-time 42.0000")
	assert.Regexp(t, expect, output, "bmad exec time meta-stat is reported")
	expect = regexp.MustCompile("SAMPLE \\d+ test01.example.com:bmad:test_check:exec-time 42.000")
	assert.Regexp(t, expect, output, "bmad check exec time meta-stat is reported")
	expect = regexp.MustCompile("COUNTER \\d+ test01.example.com:bmad:checks")
	assert.NotRegexp(t, expect, output, "bmad check counter meta-stat is not reported")
	expect = regexp.MustCompile("SAMPLE \\d+ test01.example.com:bmad:latency 0.0240")
	assert.NotRegexp(t, expect, output, "bmad latency meta-stat is not reported")

	check.Bulk     = "true"
	check.attempts = 1
	check.Retries  = 3
	output = check.test_submission(t, false, 1024)
	expect = regexp.MustCompile("^myoutput")
	assert.Regexp(t, expect, output, "Bulk check with fewer attempts than retries submits status")

	check.Bulk = "false"
	output = check.test_submission(t, false, 1024)
	expect = regexp.MustCompile("^myoutput")
	assert.NotRegexp(t, expect, output, "Non-bulk check with fewer attempts than retries doesn't submit status")
	expect = regexp.MustCompile("SAMPLE \\d+ test01.example.com:bmad:exec-time 42.0000")
	assert.Regexp(t, expect, output, "meta-stats are reported despite attempts less than max retries")

	check.attempts = 3
	output = check.test_submission(t, false, 1024)
	expect = regexp.MustCompile("^myoutput")
	assert.Regexp(t, expect, output, "Non-bulk check with more attempts than retries submits status")
}

func (check *Check) test_submission(t *testing.T, full_stats bool, buf_len int) (string) {
	if buf_len == 0 {
		buf_len = 1024
	}
	r, w, err := os.Pipe()
	writer = w
	defer func () { writer = nil }()

	check.Submit(full_stats)
	var buffer []byte
	buffer = make([]byte, buf_len)
	n, err := r.Read(buffer)
	assert.NoError(t, err, "No errors reading output")
	return string(buffer[0:n])
}

func Test_Output(t *testing.T) {
	check := Check{
		output: "this is my output\nline two.",
	}

	assert.Equal(t, "this is my output\nline two.", check.Output(), "check.Output() returns check output")
}
