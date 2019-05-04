package sentry

import (
	"bufio"
	"io/ioutil"
  "os"
  "fmt"
	"encoding/json"
	"strings"
)

func GetModules() (map[string]string, error) {
  modules := make(map[string]string)

  if checkFileExist("go.mod") {
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

  if checkFileExist("vendor") {

    // Priority given to vendor created by modules
    if checkFileExist("vendor/modules.txt") {
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

    if checkFileExist("vendor/vendor.json") {
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
	}

  return nil, fmt.Errorf("Module integration failed")
}
