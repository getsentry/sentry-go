package sentry

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"runtime"
	"strings"
)

type modulesIntegration struct{}

var _modulesCache map[string]string

func (mi modulesIntegration) Name() string {
	return "Modules"
}

func (mi modulesIntegration) SetupOnce() {
	AddGlobalEventProcessor(mi.processor)
}

func (mi modulesIntegration) processor(event *Event, hint *EventHint) *Event {
	// Run the integration only on the Client that registered it
	if CurrentHub().GetIntegration(mi.Name()) == nil {
		return event
	}

	if event.Modules == nil {
		event.Modules = extractModules()
	}

	return event
}

func extractModules() map[string]string {
	if _modulesCache != nil {
		return _modulesCache
	}

	extractedModules, err := getModules()
	if err != nil {
		Logger.Printf("ModuleIntegration wasn't able to extract modules: %v\n", err)
		return nil
	}

	_modulesCache = extractedModules

	return extractedModules
}

func getModules() (map[string]string, error) {
	if fileExists("go.mod") {
		return getModulesFromMod()
	}

	if fileExists("vendor") {
		// Priority given to vendor created by modules
		if fileExists("vendor/modules.txt") {
			return getModulesFromVendorTxt()
		}

		if fileExists("vendor/vendor.json") {
			return getModulesFromVendorJSON()
		}
	}

	return nil, fmt.Errorf("module integration failed")
}

func getModulesFromMod() (map[string]string, error) {
	modules := make(map[string]string)

	file, err := os.Open("go.mod")
	if err != nil {
		return nil, fmt.Errorf("unable to open mod file")
	}

	defer file.Close()

	areModulesPresent := false

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		splits := strings.Split(scanner.Text(), " ")

		if splits[0] == "require" {
			areModulesPresent = true

			// Mod file has only 1 dependency
			if len(splits) > 2 {
				modules[strings.TrimSpace(splits[1])] = splits[2]
				return modules, nil
			}
		} else if areModulesPresent && splits[0] != ")" {
			modules[strings.TrimSpace(splits[0])] = splits[1]
		}
	}

	if scannerErr := scanner.Err(); scannerErr != nil {
		return nil, scannerErr
	}

	return modules, nil
}

func getModulesFromVendorTxt() (map[string]string, error) {
	modules := make(map[string]string)

	file, err := os.Open("vendor/modules.txt")
	if err != nil {
		return nil, fmt.Errorf("unable to open vendor/modules.txt")
	}

	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		splits := strings.Split(scanner.Text(), " ")

		if splits[0] == "#" {
			modules[splits[1]] = splits[2]
		}
	}

	if scannerErr := scanner.Err(); scannerErr != nil {
		return nil, scannerErr
	}

	return modules, nil
}

func getModulesFromVendorJSON() (map[string]string, error) {
	modules := make(map[string]string)

	file, err := ioutil.ReadFile("vendor/vendor.json")

	if err != nil {
		return nil, fmt.Errorf("unable to open vendor/vendor.json")
	}

	var vendor map[string]interface{}
	if unmarshalErr := json.Unmarshal(file, &vendor); unmarshalErr != nil {
		return nil, unmarshalErr
	}

	packages := vendor["package"].([]interface{})

	// To avoid iterative dependencies, TODO: Change of default value
	lastPath := "\n"

	for _, value := range packages {
		path := value.(map[string]interface{})["path"].(string)

		if !strings.Contains(path, lastPath) {
			// No versions are available through vendor.json
			modules[path] = ""
			lastPath = path
		}
	}

	return modules, nil
}

type environmentIntegration struct{}

func (ei environmentIntegration) Name() string {
	return "Environment"
}

func (ei environmentIntegration) SetupOnce() {
	AddGlobalEventProcessor(ei.processor)
}

func (ei environmentIntegration) processor(event *Event, hint *EventHint) *Event {
	// Run the integration only on the Client that registered it
	if CurrentHub().GetIntegration(ei.Name()) == nil {
		return event
	}

	if event.Contexts == nil {
		event.Contexts = make(map[string]interface{})
	}

	event.Contexts["device"] = map[string]interface{}{
		"arch":    runtime.GOARCH,
		"num_cpu": runtime.NumCPU(),
	}

	event.Contexts["os"] = map[string]interface{}{
		"name": runtime.GOOS,
	}

	event.Contexts["runtime"] = map[string]interface{}{
		"name":    "go",
		"version": runtime.Version(),
	}

	return event
}

type requestIntegration struct{}

func (ri requestIntegration) Name() string {
	return "Request"
}

func (ri requestIntegration) SetupOnce() {
	AddGlobalEventProcessor(ri.processor)
}

func (ri requestIntegration) processor(event *Event, hint *EventHint) *Event {
	// Run the integration only on the Client that registered it
	if CurrentHub().GetIntegration(ri.Name()) == nil {
		return event
	}

	if hint == nil {
		return event
	}

	if hint.Request != nil {
		return ri.fillEvent(event, hint.Request)
	}

	if hint.Context == nil {
		return event
	}

	if request, ok := hint.Context.Value(RequestContextKey).(*http.Request); ok {
		return ri.fillEvent(event, request)
	}

	return event
}

func (ri requestIntegration) fillEvent(event *Event, request *http.Request) *Event {
	event.Request.Method = request.Method
	event.Request.Cookies = request.Cookies()
	event.Request.Headers = request.Header
	event.Request.URL = request.URL.String()
	event.Request.QueryString = request.URL.RawQuery
	if body, err := ioutil.ReadAll(request.Body); err == nil {
		event.Request.Data = string(body)
	}
	return event
}
