package bma

import "io/ioutil"
import "os"
import "github.com/stretchr/testify/assert"
import "testing"
import "time"

func Test_SendToBolo(t *testing.T) {
	err := SendToBolo("this should fail")
	assert.EqualError(t, err, "invalid argument", "Writing to invalid pipe returns error")

	var buffer []byte
	buffer = make([]byte, 25)
	r, w, err := os.Pipe()
	writer = w
	defer func () { writer =  nil }()

	w.Write([]byte("Test message through bolo"))
	n, err := r.Read(buffer)
	assert.Equal(t, "Test message through bolo", string(buffer), "We're able to read what we wrote")
	assert.Equal(t, 25, n, "Read 25 bytes")
	assert.NoError(t, err, "No errors from reading")
}

func Test_LifeOfBolo(t *testing.T) {
	cfg = &Config{
		Send_bolo: "t/bin/not_send_bolo",
	}
	writer = nil
	send2bolo = nil

	err := ConnectToBolo()
	assert.EqualError(t, err,
		"exec: \"t/bin/not_send_bolo\": stat t/bin/not_send_bolo: no such file or directory",
		"ConnectToBolo on bad command fails")
	assert.Nil(t, writer, "No writer yet")
	assert.Nil(t, send2bolo, "No send2bolo yet")

	cfg.Send_bolo = "`unparseable"
	err = ConnectToBolo()
	assert.EqualError(t, err, "invalid command line string", "ConnectToBolo on unparseable command fails")
	assert.Nil(t, writer, "No writer yet")
	assert.Nil(t, send2bolo, "No send2bolo yet")

	os.Mkdir("t/tmp", 0755)
	os.Remove("t/tmp/bolo.out")
	_, err = os.Stat("t/tmp/bolo.out");
	assert.True(t, os.IsNotExist(err), "bolo.out temp file is gone, test can start")

	cfg.Send_bolo = "t/bin/send_bolo"
	err = ConnectToBolo()
	assert.NotNil(t, writer, "We have a writer!")
	assert.NotNil(t, send2bolo, "We have send2bolo!")
	assert.True(t, send2bolo.Process.Pid > 1, "send2bolo has a pid")

	err = SendToBolo("Test message\n")
	time.Sleep(100 * time.Millisecond) // wait for buffers to be read + files to write
	assert.NoError(t, err, "No error on sending a message to SendToBolo")

	DisconnectFromBolo()
	time.Sleep(100 * time.Millisecond) // wait for process to die and be reaped
	assert.Nil(t, send2bolo, "send2bolo was reaped")
	assert.NotPanics(t, DisconnectFromBolo, "Disconnecting from bolo multiple times is safe")

	got, err := ioutil.ReadFile("t/tmp/bolo.out")
	assert.NoError(t, err, "Able to read data from send_bolo output")
	assert.Equal(t, "Test message\n", string(got), "Read in correct data from send_bolo output")
}
