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
	"bytes"
	"encoding/csv"
	"log"
	"strings"
	"time"
	"strconv"
)

type CSVParser struct {
	App           string
	AppVer        string
	Filename      string
	Hostname      string
	FieldsOrder   []string
	Table         []Attribute
	Delimiter     rune
	SkipHeader    bool
	CheckNHeaders int
}

func NewCSVParser(app, appVer, filename, hostname string, fieldsOrder []string, defaultTable []Attribute, options []string) *CSVParser {

	// Init defaults
	delimiter     := rune(',')
	skipHeader    := false
	checkNHeaders := 2
	parsedOptions := ParseOptions(options)

	for k, v := range parsedOptions {
		var err error
		switch k {
		case "delimiter":
			runes := []rune(v)
			if len(runes) > 0 {
				delimiter = runes[0]
			}
		case "skipheader" :
			skipHeader, err = strconv.ParseBool(v)
			if err != nil {
				log.Printf(err.Error())
			}
		case "checknheaders" :
			checkNHeaders, err = strconv.Atoi(v)
			if err != nil {
				log.Printf(err.Error())
			}
		}
	}

	return &CSVParser{
		App:           app,
		AppVer:        appVer,
		Filename:      filename,
		Hostname:      hostname,
		FieldsOrder:   fieldsOrder,
		Table:         defaultTable,
		Delimiter:     delimiter,
		SkipHeader:    skipHeader,
		CheckNHeaders: checkNHeaders,
	}
}

func (p *CSVParser) Init(defaults, fieldMappings map[string]string, FieldsOrder []string, defaultTable []Attribute) {
}

func (p *CSVParser) GetTable() []Attribute {
	return p.Table
}

func (p *CSVParser) Defaults() map[string]string {

	d := make(map[string]string)
	for _, k := range p.Table {
		d[k.Key] = "\\N"
	}

	d["app"] = p.App
	d["app_ver"] = p.AppVer
	d["filename"] = p.Filename
	d["hostname"] = p.Hostname
	d["ingest_datetime"] = time.Now().UTC().Format(ISO_8601)
	d["event_datetime"] = d["ingest_datetime"]

	return d
}

func (p *CSVParser) Parse(line string) (map[string]string, error) {

	result := p.Defaults()
	r := csv.NewReader(strings.NewReader(line))
	r.Comma = p.Delimiter
	var cleanLogLine bytes.Buffer

	record, err := r.Read()

	if err != nil {
		log.Printf(err.Error())
		return result, err
	}

	if len(record) != len(p.FieldsOrder) {
		return result, ErrCSVFieldsOrderDoNotMatch
	}

	// Skip Headers
	// Cannot nest lenEval in check for p.SkipHeader - https://github.com/golang/go/issues/18664
	lenEval := len(strings.Join(p.FieldsOrder[:p.CheckNHeaders], string(p.Delimiter)))

	if p.SkipHeader &&  strings.ToLower(strings.Replace(line, "\"", "", -1 ))[:lenEval] == strings.Join(p.FieldsOrder[:p.CheckNHeaders], string(p.Delimiter)) {
		return result, nil
	}

	for i, field := range p.FieldsOrder {
		value := record[i]

		skipField := false
		if field == "" {
			skipField = true
		}
		_, ok := result[field]

		if skipField || !ok {
			cleanLogLine.WriteString(value)
			cleanLogLine.WriteString(" ")
		}

		if isNull(value) {
			result[field] = "\\N"
		} else {
			result[field] = value
		}
	}

	srcByte := cleanupPairs.ReplaceAll(cleanLogLine.Bytes(), []byte{})
	srcByte = cleanupSpaces.ReplaceAll(srcByte, []byte{})
	result["log_line"] = strings.TrimSpace(string(srcByte))

	return result, err

}
