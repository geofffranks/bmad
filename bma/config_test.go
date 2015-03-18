package bma

import "testing"
import "github.com/stretchr/testify/assert"
import "errors"
import "os"
import "time"

func TestHostname(t *testing.T) {
	orig_osh := os_hostname
	orig_nlh := net_lookuphost
	orig_nla := net_lookupaddr

	var mock_hostname string
	var mock_herror error
	os_hostname = func () (string, error) {
		return mock_hostname, mock_herror
	}
	defer func () { os_hostname = orig_osh }()

	var mock_addrs []string
	var mock_nherror error
	net_lookuphost = func (string) ([]string, error) {
		return mock_addrs, mock_nherror
	}
	defer func () { net_lookuphost = orig_nlh }()

	var mock_hosts []string
	var mock_naerror error
	net_lookupaddr = func (string) ([]string, error) {
		return mock_hosts, mock_naerror
	}
	defer func () { net_lookupaddr = orig_nla }()

	mock_hostname = "test01.example.com"
	assert.Equal(t, "test01.example.com", hostname(),
		"hostname() should use os.Hostname() if it has a '.'")

	mock_herror = errors.New("Couldn't look up hostname")
	assert.Equal(t, "unknown", hostname(),
		"hostname() handles failures in os.Hostname()")

	mock_herror   = nil
	mock_hostname = "test01"
	mock_addrs    = []string{"127.0.0.1"}
	mock_hosts    = []string{"test02", "test03", "test01.example.com"}
	assert.Equal(t, "test01.example.com", hostname(),
		"hostname() finds the first name with a '.' in it from lookupaddr")

	mock_nherror = errors.New("couldn't lookup hostname")
	assert.Equal(t, "test01", hostname(),
		"hostname() falls back to os.Hostname() on net.LookupHost() failure")

	mock_nherror = nil
	mock_naerror = errors.New("couldn't lookup addr")
	assert.Equal(t, "test01", hostname(),
		"hostname() falls back to os.Hostname on net.LookupHost() failure")

	mock_hosts   = []string{"test02", "test03"}
	mock_naerror = nil
	assert.Equal(t, "test01", hostname(),
		"hostname() falls back to os.Hostname() if no fqdns are found")
}

func TestDefaultConfig(t *testing.T) {
	orig_osh := os_hostname
	os_hostname = func () (string, error) {
		return "test01.example.com", nil
	}
	defer func () { os_hostname = orig_osh }()
	expect := Config{
		Every:       300,
		Retry_every: 60,
		Retries:     1,
		Timeout:     45,
		Send_bolo:   "send_bolo -t stream",
		Host:        "test01.example.com",
		Include_dir: "/etc/bmad.d",
		Checks:      map[string]*Check{},
		Env:         map[string]string{},
	}
	assert.Equal(t, &expect, default_config(), "default_config() returns expected config")
}

func TestLoadConfig(t *testing.T) {
	orig_osh := os_hostname
	os_hostname = func () (string, error) {
		return "test01.example.com", nil
	}
	defer func () { os_hostname = orig_osh }()
	orig_first_run := first_run
	first_run = func (i int64) (time.Time) { return time.Unix(42,0) }
	defer func () { first_run = orig_first_run }()

	cfg = nil // Reset cfg
	_, err := LoadConfig("doesntexist")
	assert.IsType(t, &os.PathError{}, err, "LoadConfig() on non-existent config file returns an error")

	cfg = nil // Reset cfg
	_, err = LoadConfig("t/data/bad.yml")
	assert.EqualError(t, err, "YAML error: line 1: found unexpected end of stream",
		"LoadConfig() on bad yaml returns an error")

	cfg = nil // Reset cfg
	got, err := LoadConfig("t/data/basic.yml")
	assert.Nil(t, err, "LoadConfig() on valid yaml doesn't return an error")
	expect := default_config()
	expect.Send_bolo   = "t/bin/send_bolo"
	expect.Include_dir = "t/data/bmad.empty"
	expect.Every       = 10
	expect.Retry_every = 6
	expect.Retries     = 2
	expect.Timeout     = 5
	expect.Log.Level   = "warning"
	expect.Log.Type    = "file"
	expect.Log.File    = "/dev/null"
	expect.Checks["first"] = &Check{
		Command:     "echo \"success\"",
		Every:       10,
		Retry_every: 6,
		Retries:     2,
		Timeout:     5,
		Env:         map[string]string{},
		Name:        "first",
		cmd_args:    []string{"echo", "success"},
		next_run:    time.Unix(42,0),
	}
	// There is a "second" key in the yaml file, with no check command
	// found. It should be ingored on config load, so we don't run it needlessly
	// Do not expect it.

	assert.Equal(t, expect, got, "LoadConfig('t/data/basic.yml') provided expected config")

	cfg = nil // Reset cfg
	os.Chmod("t/data/bmad.d/unreadable.conf", 0200)
	got, err = LoadConfig("t/data/extended.yml")
	os.Chmod("t/data/bmad.d/unreadable.conf", 0644)
	// This directory should have both a more.conf (parseable), and a bad.conf (unparseable)
	expect.Include_dir = "t/data/bmad.d"
	expect.Checks["third"] = &Check{
		Command:     "echo \"third success\"",
		Every:       30,
		Retry_every: 25,
		Retries:     10,
		Timeout:     20,
		Env:         map[string]string{},
		Name:        "third",
		cmd_args:    []string{"echo", "third success"},
		next_run:    time.Unix(42,0),
	}
	// There is a redefinition of "second" in t/data/bmad.d/more.yml.
	// Unfortunately, bmad takes the first earliest found definition,
	// and skips the rest. Since the earliest was defined improperly,
	// it's skipped over still. Don't expect it.

	assert.Equal(t, expect, got, "LoadConfig('t/data/extended.yml') provided expected config")

	//FIXME: test reconfiguration
}
