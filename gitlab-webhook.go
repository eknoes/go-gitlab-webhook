package main

import (
	"net/http"
	"encoding/json"
	"io/ioutil"
	"os/exec"
	"os"
	"log"
	"errors"
	"strconv"
	"os/signal"
	"syscall"
)

//GitlabRepository represents repository information from the webhook
type GitlabProject struct {
	Name, Web_Url, Description, Path_With_Namespace string
}

//Commit represents commit information from the webhook
type Commit struct {
	Id, Message, Timestamp, Url string
	Author                      Author
}

//Build represents build information from the webhook
type Build struct {
	Stage, Status string
}

//Author represents author information from the webhook
type Author struct {
	Name, Email string
}

//Webhook represents push information from the webhook
type Webhook struct {
	Object_Kind, Before, After, Ref, User_name string
	User_id, Project_id                        int
	Project                                    GitlabProject
	Commits                                    []Commit
	Builds                                     []Build
	Total_commits_count                        int
}

//ConfigRepository represents a repository from the config file
type ConfigCommand struct {
	Stage, Cmd string
}

type ConfigRepository struct {
	Name     string
	Commands []ConfigCommand
}

//Config represents the config file
type Config struct {
	Logfile      string
	Address      string
	Port         int64
	Repositories []ConfigRepository
	Secret       string
}

func PanicIf(err error, what ...string) {
	if (err != nil) {
		if (len(what) == 0) {
			panic(err)
		}

		panic(errors.New(err.Error() + what[0]))
	}
}

var config Config
var configFile string

func main() {
	args := os.Args

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGHUP)

	go func() {
		<-sigc
		config = loadConfig(configFile)
		log.Println("config reloaded")
	}()

	//if we have a "real" argument we take this as conf path to the config file
	if (len(args) > 1) {
		configFile = args[1]
	} else {
		configFile = "config.json"
	}

	//load config
	config := loadConfig(configFile)

	//open log file
	writer, err := os.OpenFile(config.Logfile, os.O_RDWR | os.O_APPEND | os.O_CREATE, 0666)
	PanicIf(err)

	//close logfile on exit
	defer func() {
		writer.Close()
	}()

	//setting logging output
	log.SetOutput(writer)

	//setting handler
	http.HandleFunc("/", hookHandler)

	address := config.Address + ":" + strconv.FormatInt(config.Port, 10)

	log.Println("Listening on " + address)

	//starting server
	err = http.ListenAndServe(address, nil)
	if (err != nil) {
		log.Println(err)
	}
}

func loadConfig(configFile string) Config {
	var file, err = os.Open(configFile)
	PanicIf(err)

	// close file on exit and check for its returned error
	defer func() {
		err := file.Close()
		PanicIf(err)
	}()

	fi, err := file.Stat()

	buffer := make([]byte, fi.Size())
	count := 0

	count, err = file.Read(buffer)
	PanicIf(err)

	err = json.Unmarshal(buffer[:count], &config)
	PanicIf(err)

	return config
}

func hookHandler(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if r := recover(); r != nil {
			log.Println(r)
		}
	}()

	//Check secret token
	if config.Secret != "" {
		if config.Secret != r.Header.Get("X-Gitlab-Token") {
			PanicIf(errors.New("Invalid token"), "X-Gitlab-Token is different than specified in config")
		}
	}

	var hook Webhook

	//read request body
	var data, err = ioutil.ReadAll(r.Body)
	PanicIf(err, "while reading request")

	//unmarshal request body
	err = json.Unmarshal(data, &hook)
	PanicIf(err, "while unmarshaling request")

	log.Println("Got Webhook for " + hook.Project.Path_With_Namespace + ": " + hook.Object_Kind)
	//find matching config for repository name

	var found = false
	for _, repo := range config.Repositories {
		if (repo.Name != hook.Project.Path_With_Namespace) {
			continue
		}

		//execute commands for repository
		for _, cmd := range repo.Commands {

			for _, build := range hook.Builds {
				if (build.Status == "success" && build.Stage == cmd.Stage) {
					found = true
					var command = exec.Command(cmd.Cmd)
					out, err := command.Output()
					if (err != nil) {
						log.Println(err)
					} else {
						log.Println("Executed (Stage " + cmd.Stage + "): " + cmd.Cmd)
						log.Println("Output: " + string(out))
					}
				}
			}

		}
	}

	if(!found) {
		log.Println("Could not find sth to execute")
	}
}
