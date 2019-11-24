package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/flynn/go-shlex"
	dockerapi "github.com/fsouza/go-dockerclient"
)

var debug = flag.Bool("d", false, "debug mode displays handler output")
var shell = flag.Bool("s", false, "run handler via SHELL")

var skipInspect = map[string]bool{
	"destroy": true,
	"untag":   true,
	"delete":  true,
}

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %v [options] <hook-handler>\n\n", os.Args[0])
		flag.PrintDefaults()
	}
}

func assert(err error) {
	if err != nil {
		log.Fatal("fatal: ", err)
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

func inspect(docker *dockerapi.Client, id string) *bytes.Buffer {
	var b bytes.Buffer
	container, err := docker.InspectContainer(id)
	if err != nil {
		log.Println("warn: unable to inspect container:", id[:12], err)
		return &b
	}
	data, err := json.Marshal(container)
	if err != nil {
		log.Println("warn: unable to marshal container data:", id[:12], err)
		return &b
	}
	b.Write(data)
	return &b
}

func trigger(hook []string, event, id string, docker *dockerapi.Client) {
	log.Println("info: trigger:", id[:12], event)
	hook = append(hook, event, id)
	var cmd *exec.Cmd
	if *shell && os.Getenv("SHELL") != "" {
		cmd = exec.Command(os.Getenv("SHELL"), "-c", strings.Join(hook, " "))
	} else {
		cmd = exec.Command(hook[0], hook[1:]...)
	}
	if !skipInspect[event] {
		cmd.Stdin = inspect(docker, id)
	}
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
	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(64)
	}

	hook, err := shlex.Split(flag.Arg(0))
	if err != nil {
		log.Fatalln("fatal: unable to parse handler command:", err)
	}
	hook[0], err = filepath.Abs(hook[0])
	if err != nil {
		log.Fatalln("fatal: invalid handler executable path:", err)
	}

	if os.Getenv("DOCKER_HOST") == "" {
		assert(os.Setenv("DOCKER_HOST", "unix:///var/run/docker.sock"))
	}
	docker, err := dockerapi.NewClientFromEnv()
	assert(err)

	containers, err := docker.ListContainers(dockerapi.ListContainersOptions{})
	assert(err)
	for _, listing := range containers {
		trigger(hook, "exists", listing.ID, docker)
	}

	events := make(chan *dockerapi.APIEvents)
	assert(docker.AddEventListener(events))
	log.Println("info: listening for Docker events...")
	for msg := range events {
		go trigger(hook, msg.Status, msg.ID, docker)
	}

	log.Fatal("fatal: docker event loop closed") // todo: reconnect?
}
