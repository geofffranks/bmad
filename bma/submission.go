package bma

import "github.com/geofffranks/bmad/log"
import "os"
import "os/exec"
import shellwords "github.com/mattn/go-shellwords"

var writer *os.File

//FIXME: use zmq directly

// Launches a child process to hold open a ZMQ connection
// to the upstream bolo server (send_bolo should take care
// of the configuration for how to connect). Upon termination
// this process will be respawned.
//
// Currently, if the send_bolo configuration directive for bmad
// is updated on a config reload, the send_bolo process will not
// be respawned. A full-daemon restart is required to make use
// of the new send_bolo configuration
func ConnectToBolo() (error) {
	args, err := shellwords.Parse(cfg.Send_bolo)
	if err != nil {
		panic(err)
	}
	log.Debug("Spawning bolo submitter:  %#v", args)
	proc := exec.Command(args[0], args[1:]...)
	r, w, err := os.Pipe()
	if (err != nil) {
		panic(err)
	}
	proc.Stdin  = r
	writer = w
	err = proc.Start()
	log.Debug("send_bolo: %#v", proc)
	if (err != nil) {
		panic(err)
	}
	go proc.Wait()
	return nil
}

// Sends an individual message from check output to bolo,
// via the send_bolo child process, spawned in ConnectToBolo()
func SendToBolo(msg string) (error) {
	if _, err := writer.Write([]byte(msg)); err != nil {
		return err
	}

	return nil
}
