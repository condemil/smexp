package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/ec2metadata"
	"github.com/aws/aws-sdk-go-v2/aws/external"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	flags "github.com/jessevdk/go-flags"
	yaml "gopkg.in/yaml.v2"
)

var (
	// Version of the application, populated during deployment
	Version = "dev"

	// GitHash of the application, populated during deployment
	GitHash = "undefined"

	// BuildTime of the application, populated during deployment
	BuildTime = "undefined"
)

const (
	// EXOK successful termination
	EXOK = 0

	// EXUSAGE command line usage error
	EXUSAGE = 64

	// EXDATAERR data format error
	EXDATAERR = 65

	// EXIOERR input/output error
	EXIOERR = 74

	// EXCONFIG configuration error
	EXCONFIG = 78
)

var options struct {
	FileType  string `short:"t" long:"type" choice:"json" choice:"yaml" description:"output file type, file extension is used if not specified"`
	Version   func() `short:"v" long:"version" description:"print version and exit"`
	Arguments struct {
		SecretName string `positional-arg-name:"SECRET" description:"secret name"`
		FileName   string `positional-arg-name:"FILE" description:"output file"`
	} `positional-args:"yes" required:"true"`
}

func main() {
	options.Version = func() {
		fmt.Printf("Version:    %s\nGit Hash:   %s\nBuild Time: %s\n", Version, GitHash, BuildTime)
		os.Exit(EXOK)
	}

	parser := flags.NewParser(&options, flags.Default)
	parser.LongDescription = "Retrieve secret from AWS Secrets Manager and save to file. JSON and YAML formats supported."

	if _, err := parser.Parse(); err != nil {
		flagsErr, ok := err.(*flags.Error)

		if flagsErr.Type == flags.ErrHelp {
			// -h called, print help and exit
			os.Exit(EXUSAGE)
		}

		if !ok || flagsErr.Type != flags.ErrHelp {
			//
			fmt.Println()
			parser.WriteHelp(os.Stdout)
			fmt.Println()
			os.Exit(EXUSAGE)
		}
	}

	secret := retrieveSecret(options.Arguments.SecretName)

	ext := ""

	if options.FileType != "" {
		ext = "." + options.FileType
	} else {
		ext = filepath.Ext(options.Arguments.FileName)
	}

	var result []byte

	if ext == ".json" {
		result = formatJSON(secret)
	} else if ext == ".yaml" || ext == ".yml" {
		result = jsonToYAML(secret)
	} else {
		fmt.Fprintf(os.Stderr, "unsupported file type: %v\n", ext)
		os.Exit(EXUSAGE)
	}

	writeFile(options.Arguments.FileName, result)
}

func retrieveSecret(name string) []byte {
	cfg := loadAWSConfig()
	smc := secretsmanager.New(cfg)
	input := &secretsmanager.GetSecretValueInput{
		SecretId: &name,
	}

	req := smc.GetSecretValueRequest(input)
	result, err := req.Send()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(EXUSAGE)

	}

	return []byte(*result.SecretString)
}

func loadAWSConfig() aws.Config {
	cfg, err := external.LoadDefaultAWSConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "aws: failed to load config, %v\n", err)
		os.Exit(EXCONFIG)
	}

	if cfg.Region == "" {
		// try to get region from ec2 instance metadata
		metadata := ec2metadata.New(cfg)

		region := make(chan string, 1)
		go func() {
			r, err := metadata.Region()
			if err != nil {
				fmt.Fprintf(os.Stderr, "aws: failed to load region from instance metadata, %v\n", err)
				os.Exit(EXCONFIG)
			}
			region <- r
		}()

		select {
		case res := <-region:
			cfg.Region = res
		case <-time.After(5 * time.Second):
			fmt.Fprintf(os.Stderr, "aws: region not found\n")
			os.Exit(EXCONFIG)
		}
	}

	return cfg
}

func writeFile(name string, data []byte) {
	err := ioutil.WriteFile(name, data, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(EXIOERR)
	}
}

func formatJSON(j []byte) []byte {
	dst := &bytes.Buffer{}

	if err := json.Indent(dst, j, "", "    "); err != nil {
		fmt.Fprintf(os.Stderr, "json: problem formatting, %v\n", err)
		os.Exit(EXDATAERR)
	}

	return dst.Bytes()
}

func jsonToYAML(j []byte) (result []byte) {
	var jsonObj interface{}

	err := yaml.Unmarshal(j, &jsonObj)

	if err == nil {
		result, err = yaml.Marshal(jsonObj)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "yaml: failed to convert from json, %v\n", err)
		os.Exit(EXDATAERR)
	}

	return result
}
