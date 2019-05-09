package sentry

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"sentry"
)

type ModulesIntegration struct{}

var _modulesCache map[string]string

func (mi ModulesIntegration) Name() string {
	return "Modules"
}

func (mi ModulesIntegration) SetupOnce() {
  sentry.AddGlobalEventProcessor(mi.processor)
}

func (mi ModulesIntegration) processor(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
  // Run the integration only on the Client that registered it
  if sentry.CurrentHub().GetIntegration(mi.Name()) == nil {
    return event
  }

  if event.Modules == nil {
    event.Modules = extractModules()
  }

  return event
}

func extractModules() (map[string]string) {
	if _modulesCache != nil {
		return _modulesCache
	}

	extractedModules, err := getModules()
	if err != nil {
    debugger.Printf("ModuleIntegration wasn't able to extract modules: %v\n", err)
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
			return getModulesFromVendorJson()
		}
	}

	return nil, fmt.Errorf("Module integration failed")
}

func getModulesFromMod() (map[string]string, error) {
	modules := make(map[string]string)

	file, err := os.Open("go.mod")
	defer file.Close()

	if err != nil {
		return nil, fmt.Errorf("Unable to open mod file")
	}

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
	defer file.Close()

	if err != nil {
		return nil, fmt.Errorf("Unable to open vendor/modules.txt")
	}

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

func getModulesFromVendorJson() (map[string]string, error) {
	modules := make(map[string]string)

	file, err := ioutil.ReadFile("vendor/vendor.json")

	if err != nil {
		return nil, fmt.Errorf("Unable to open vendor/vendor.json")
	}

	var vendor map[string]interface{}
	json.Unmarshal(file, &vendor)
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
