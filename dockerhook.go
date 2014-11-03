package main

import (
	"log"
	"os"
	"flag"
	"path/filepath"
	"os/exec"
	"syscall"
	"bytes"
	"encoding/json"

	"github.com/flynn/go-shlex"
	dockerapi "github.com/fsouza/go-dockerclient"
)

var debug = flag.Bool("d", false, "debug mode displays handler output")
var env = flag.Bool("e", false, "pass environment to handler")
var shell = flag.Bool("s", false, "run handler via SHELL")

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %v [options] <hook-handler>\n\n", os.Args[0])
		flag.PrintDefaults()
	}
}

func assert(err error) {
	if err != nil {
		log.Fatal("fatal:", err)
	}
}

func getopt(name, def string) string {
	if env := os.Getenv(name); env != "" {
		return env
	}
	return def
}

func exitStatus(err error) (int, error) {
	if err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			// There is no platform independent way to retrieve
			// the exit code, but the following will work on Unix
			if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
				return int(status.ExitStatus()), nil
			}
		}
		return 0, err
	}
	return 0, nil
}

func inspect(docker *dockerapi.Client, id string) bytes.Buffer {
	var b bytes.Buffer
	container, err := docker.InspectContainer(id)
	if err != nil {
		log.Println("warn: unable to inspect container:", id[:12], err)
		return b
	}
	data, err := json.Marshal(m)
	if err != nil {
		log.Println("warn: unable to marshal container data:", id[:12], err)
		return b
	}
	b.Write(data)
	return b
}

func trigger(hook []string, event, id string, data bytes.Buffer) {
	hook = append(hook, event, id)
	var cmd exec.Cmd
	if *shell {
		cmd = exec.Command(os.Getenv("SHELL"), "-c", strings.Join(hook, " "))
	} else {	
		cmd = exec.Command(hook[0], hook[1:]...)
	}
	if !*env {
		cmd.Env = []string{}	
	}
	cmd.Stdin = data
	if *debug {
		cmd.Stdout = os.Stdout // TODO: wrap in log output
	}
	cmd.Stderr = os.Stderr // TODO: wrap in log output
	status, err := exitStatus(cmd.Run())
	if err != nil {
		log.Println("error:", event, status, err)
	}
}

func main() {
	flag.Parse()
	if flag.NArg() < 2 {
		flag.Usage()
		os.Exit(64)
	}

	hook, err := shlex.Split(flag.Arg(1))
	if err != nil {
		log.Fatalln("fatal: unable to parse handler command:", err)
	}
	hook[0], err = filepath.Abs(hook[0])
	if err != nil {
		log.Fatalln("fatal: invalid handler executable path:", err)
	}

	docker, err := dockerapi.NewClient(getopt("DOCKER_HOST", "unix:///var/run/docker.sock"))
	assert(err)

	containers, err := docker.ListContainers(dockerapi.ListContainersOptions{})
	assert(err)
	for _, listing := range containers {
		trigger(hook, "exists", listing.ID, inspect(docker, listing.ID))
	}

	events := make(chan *dockerapi.APIEvents)
	assert(docker.AddEventListener(events))
	log.Println("info: listening for Docker events...")
	for msg := range events {
		log.Println("info: event:", msg.Status, msg.ID[:12])
		go trigger(hook, msg.Status, msg.ID, inspect(docker, msg.ID))
	}

	log.Fatal("fatal: docker event loop closed") // todo: reconnect?
}