package bma

import "os"
import "os/exec"
import shellwords "github.com/mattn/go-shellwords"

var writer *os.File

//FIXME: use zmq directly
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

func SendToBolo(msg string) (error) {
	if _, err := writer.Write([]byte(msg)); err != nil {
		return err
	}

	return nil
}
