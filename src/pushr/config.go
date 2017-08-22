/*
 * Copyright (c) 2016 Yanko Bolanos
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at http://mozilla.org/MPL/2.0/.
 *
 */
package main

import (
	"fmt"
	"gopkg.in/ini.v1"
	"io"
	"io/ioutil"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

var typeLengthRegex = regexp.MustCompile(`^([^,]*),?(.*)?`)
var skipSections = regexp.MustCompile(`(record_format|DEFAULT|\w+\.\w)`)                      // only for parsing loglifes
var streamSection = regexp.MustCompile(`^stream\.(?P<reference_name>[^\.]*)$`)                // only streams
var streamRecordFormatSection = regexp.MustCompile(`^stream\.(?P<stream>.*)\.record_format$`) // only streams record format

type Attribute struct {
	Key    string
	Type   string
	Length int
}

type ConfigFile struct {
	App                string    `yaml:"app" ini:"app"`
	AppVer             string    `yaml:"app_ver" ini:"app_ver"`
	AwsAccessKey       string    `yaml:"aws_access_key" ini:"aws_access_key"`
	AwsSecretAccessKey string    `yaml:"aws_secret_access_key" ini:"aws_secret_access_key"`
	AwsRegion          string    `yaml:"aws_region" ini:"aws_region"`
	Hostname           string    `yaml:"hostname" ini:"hostname"`
	Logfiles           []Logfile `yaml:"files"`
	StreamConfigs      map[string]StreamConfig
	Streams            []StreamConfig
}

type Logfile struct {
	Filename           string            `yaml:"file" ini:"file"`
	Directory          string            `yaml:"directory" ini:"directory"`
	StreamName         string            `yaml:"stream" ini:"stream"`
	TimeFormat         string            `yaml:"time_format" ini:"time_format"`
	LineRegex          string            `yaml:"line_regex" ini:"line_regex"`
	FrontSplitRegexStr string            `yaml:"front_split_regex" ini:"front_split_regex"` // option used to split at the begining of the line instead
	ParseMode          string            `yaml:"parse_mode" ini:"parse_mode"`
	RetryFileOpen      bool              `yaml:"retry_file_open" ini:"retry_file_open"`
	FieldMappings      map[string]string `yaml:"field_mappings"`
	BufferMultiLines   bool              `yaml:"buffer_multi_lines" ini:"buffer_multi_lines"`
	FieldsOrder        []string
	FieldsOrderStr     string `ini:"fields_order"`
	LastTimestamp      time.Time
	Regex              *regexp.Regexp
	FrontSplitRegex    *regexp.Regexp
}

type StreamConfig struct {
	Name               string      `yaml:"name" ini:"name"`
	Type               string      `yaml:"type" ini:"type"`
	Url                string      `yaml:"url" ini:"url"`
	StreamApiKey       string      `yaml:"stream_api_key" ini:"stream_api_key"`
	RecordFormatString string      `ini:"record_format"`
	RecordFormat       []Attribute `yaml:"record_format"`
}

var defaultAttributes = []Attribute{
	Attribute{"app", "string", 16},
	Attribute{"app_ver", "string", 16},
	Attribute{"ingest_datetime", "timestamp", 0},
	Attribute{"event_datetime", "timestamp", 0},
	Attribute{"hostname", "string", 64},
	Attribute{"filename", "string", 256},
	Attribute{"log_level", "string", 16},
	Attribute{"device_tag", "string", 64},
	Attribute{"user_tag", "string", 64},
	Attribute{"remote_address", "string", 64},
	Attribute{"response_bytes", "integer", 0},
	Attribute{"response_ms", "double", 0},
	Attribute{"device_type", "string", 32},
	Attribute{"os", "string", 16},
	Attribute{"os_ver", "string", 16},
	Attribute{"browser", "string", 32},
	Attribute{"browser_ver", "string", 16},
	Attribute{"country", "string", 64},
	Attribute{"language", "string", 16},
	Attribute{"log_line", "string", 0},
}

func testParseConfig(configPath string) {

	configFile, err := os.Open(configPath)
	if err != nil {
		log.WithField("file", configPath).Fatalf(err.Error())
	}

	config := parseConfig(configFile)
	log.Printf("%+v", config)
}

func parseYamlConfig(src io.Reader) ConfigFile {

	data, err := ioutil.ReadAll(src)
	if err != nil {
		log.Fatalf(err.Error())
	}

	config := ConfigFile{}
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		log.Fatalf("error: %v", err)
	}

	return config

}

func parseConfig(src io.Reader) ConfigFile {

	data, err := ioutil.ReadAll(src)
	if err != nil {
		log.Fatalf(err.Error())
	}

	cfg, err := ini.Load(data)
	if err != nil {
		log.Fatal(err.Error())
	}

	var config ConfigFile
	err = cfg.Section("DEFAULT").MapTo(&config)
	if err != nil {
		log.Fatal(err.Error())
	}

	config.StreamConfigs = make(map[string]StreamConfig)
	sections := cfg.SectionStrings()

	for _, section := range sections {
		matches := streamRecordFormatSection.FindStringSubmatch(section)
		if len(matches) > 1 {
			streamName := matches[1]
			log.Printf("Parsing streamSection: %s", section)
			var stream StreamConfig
			err := cfg.Section(section).MapTo(&stream)
			if err != nil {
				log.Fatal(err.Error())
			}
			config.StreamConfigs[streamName] = stream

			// logfile := parseLogfileSection(cfg, section)
			// config.Logfiles = append(config.Logfiles, logfile)
		}
	}

	for _, section := range sections {
		matches := streamRecordFormatSection.FindStringSubmatch(section)
		if len(matches) > 1 {
			streamName := matches[1]
			if stream, ok := config.StreamConfigs[streamName]; ok {
				stream.RecordFormat = parseRecordFormat(cfg, section)
				log.Infof("Parsing streamRecordFormatSection: %s", section)
				for i := 0; i < len(stream.RecordFormat); i++ {
					log.Infof("%v -> %+v", stream.Name, stream.RecordFormat[i])
				}
				config.StreamConfigs[streamName] = stream
			} else {
				log.Fatalf("stream not found. %s", streamName)
			}

		}
	}

	for _, section := range sections {
		if !skipSections.MatchString(section) {
			logfile := parseLogfileSection(cfg, section)
			config.Logfiles = append(config.Logfiles, logfile)
		}
	}

	// config.RecordFormat = parseRecordFormat(cfg, "record_format")
	// log.Info("Record format:")
	// for i := 0; i < len(config.RecordFormat); i++ {
	// 	log.Infof("%+v", config.RecordFormat[i])
	// }

	gApp = config.App
	setAppVer(config.AppVer)

	if config.Hostname == "" {
		var err error
		config.Hostname, err = os.Hostname()
		if err != nil {
			log.Fatalf("Error getting hostname. %v", err)
		}
	}

	return config

}

func parseLogfileSection(cfg *ini.File, sectionName string) Logfile {

	var n Logfile
	err := cfg.Section(sectionName).MapTo(&n)
	if err != nil {
		log.Fatal(err.Error())
	}

	if n.FrontSplitRegexStr != "" {
		n.FrontSplitRegex = regexp.MustCompile(n.FrontSplitRegexStr)
	}

	if n.ParseMode == "regex" {
		n.Regex = regexp.MustCompile(n.LineRegex)
	} else if n.ParseMode == "json" || n.ParseMode == "date_keyvalue" {
		subsectionName := fmt.Sprintf("%s.field_mappings", sectionName)
		subsection, err := cfg.GetSection(subsectionName)
		if err != nil {
			log.Fatalf("json needs subsection with field_mappings")
		}
		keyNames := subsection.KeyStrings()
		keyValues := subsection.Keys()
		n.FieldMappings = make(map[string]string, len(keyNames))

		for i := 0; i < len(keyNames); i++ {
			n.FieldMappings[keyNames[i]] = keyValues[i].String()
		}
	} else if n.ParseMode == "csv" {
		n.FieldsOrder = parseFieldOrder(n.FieldsOrderStr)
		if len(n.FieldsOrder) == 0 {
			log.Fatalf("csv needs subsection with field_mappings")
		}
	}

	return n

}

func parseFieldOrder(fieldOrder string) []string {
	return strings.Split(fieldOrder, ",")
}

func parseRecordFormat(cfg *ini.File, section string) []Attribute {

	recordFormat := []Attribute{}
	record, err := cfg.GetSection(section)
	if err != nil {
		return defaultAttributes
	}

	keyNames := record.KeyStrings()
	keyValues := record.Keys()

	for i := 0; i < len(keyNames); i++ {
		typeLength := typeLengthRegex.FindStringSubmatch(keyValues[i].String())
		val := Attribute{Key: keyNames[i]}
		if len(typeLength) > 0 {
			val.Type = typeLength[1]
			val.Length, _ = strconv.Atoi(typeLength[2])
		}
		recordFormat = append(recordFormat, val)
	}

	return recordFormat

}
