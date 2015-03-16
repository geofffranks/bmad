package bma

import "testing"
import "github.com/stretchr/testify/assert"
import "errors"

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
