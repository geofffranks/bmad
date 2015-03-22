package bma

import "github.com/geofffranks/bmad/log"
import "os"
import "os/exec"
import shellwords "github.com/mattn/go-shellwords"
import "syscall"

var writer *os.File
var send2bolo *exec.Cmd

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
		return err
	}
	log.Debug("Spawning bolo submitter:  %#v", args)
	send2bolo = exec.Command(args[0], args[1:]...)
	r, w, err := os.Pipe()
	if (err != nil) {
		return err
	}
	send2bolo.Stdin  = r
	writer = w
	err = send2bolo.Start()
	if (err != nil) {
		writer = nil
		send2bolo = nil
		return err
	}
	log.Debug("send_bolo: %#v", send2bolo)
	go func () { send2bolo.Wait(); send2bolo = nil }()
	return nil
}

// Disconnects from bolo (terminates the send_bolo process)
// If send_bolo is no longer running, does nothing.
func DisconnectFromBolo() {
	if send2bolo == nil {
		log.Warn("Bolo disconnect requested, but send_bolo is not running")
		return
	}
	pid := send2bolo.Process.Pid
	if err:= syscall.Kill(pid, syscall.SIGTERM); err != nil {
		log.Debug("send_bolo[%d] already terminated", pid)
	}
	send2bolo = nil
}

// Sends an individual message from check output to bolo,
// via the send_bolo child process, spawned in ConnectToBolo()
func SendToBolo(msg string) (error) {
	if _, err := writer.Write([]byte(msg)); err != nil {
		return err
	}

	return nil
}
