package sentry

import (
	"fmt"
	"maps"
	"os"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/getsentry/sentry-go/internal/debuglog"
)

// ================================
// Modules Integration
// ================================

type modulesIntegration struct{}

func (mi *modulesIntegration) Name() string {
	return "Modules"
}

func (mi *modulesIntegration) SetupOnce(client *Client) {
	client.modules = sync.OnceValue(func() map[string]string {
		info, ok := debug.ReadBuildInfo()
		if !ok {
			debuglog.Print("The Modules integration is not available in binaries built without module support.")
			return nil
		}
		return extractModules(info)
	})
}

func extractModules(info *debug.BuildInfo) map[string]string {
	modules := map[string]string{
		info.Main.Path: info.Main.Version,
	}
	for _, dep := range info.Deps {
		ver := dep.Version
		if dep.Replace != nil {
			ver += fmt.Sprintf(" => %s %s", dep.Replace.Path, dep.Replace.Version)
		}
		modules[dep.Path] = strings.TrimSuffix(ver, " ")
	}
	return modules
}

// ================================
// Environment Integration
// ================================

type environmentIntegration struct{}

func (ei *environmentIntegration) Name() string {
	return "Environment"
}

func (ei *environmentIntegration) SetupOnce(client *Client) {
	client.AddEventProcessor(func(event *Event, _ *EventHint) *Event {
		event.Contexts = withEnvironment(event.Contexts)
		return event
	})
	client.transactionProcessors = append(client.transactionProcessors, func(tx *Transaction) *Transaction {
		tx.Contexts = withEnvironment(tx.Contexts)
		return tx
	})
}

// withEnvironment adds the device, os and runtime contexts to contexts.
func withEnvironment(contexts map[string]Context) map[string]Context {
	// Initialize maps as necessary.
	contextNames := []string{"device", "os", "runtime"}
	if contexts == nil {
		contexts = make(map[string]Context, len(contextNames))
	}
	for _, name := range contextNames {
		if contexts[name] == nil {
			contexts[name] = make(Context)
		}
	}

	// Set contextual information preserving existing data. For each context, if
	// the existing value is not of type map[string]interface{}, then no
	// additional information is added.
	if deviceContext, ok := contexts["device"]; ok {
		if _, ok := deviceContext["arch"]; !ok {
			deviceContext["arch"] = runtime.GOARCH
		}
		if _, ok := deviceContext["num_cpu"]; !ok {
			deviceContext["num_cpu"] = runtime.NumCPU()
		}
	}
	if osContext, ok := contexts["os"]; ok {
		if _, ok := osContext["name"]; !ok {
			osContext["name"] = runtime.GOOS
		}
	}
	if runtimeContext, ok := contexts["runtime"]; ok {
		if _, ok := runtimeContext["name"]; !ok {
			runtimeContext["name"] = "go"
		}
		if _, ok := runtimeContext["version"]; !ok {
			runtimeContext["version"] = runtime.Version()
		}
		if _, ok := runtimeContext["go_numroutines"]; !ok {
			runtimeContext["go_numroutines"] = runtime.NumGoroutine()
		}
		if _, ok := runtimeContext["go_maxprocs"]; !ok {
			runtimeContext["go_maxprocs"] = runtime.GOMAXPROCS(0)
		}
		if _, ok := runtimeContext["go_numcgocalls"]; !ok {
			runtimeContext["go_numcgocalls"] = runtime.NumCgoCall()
		}
	}
	return contexts
}

// ================================
// Ignore Errors Integration
// ================================

type ignoreErrorsIntegration struct {
	ignoreErrors []*regexp.Regexp
}

func (iei *ignoreErrorsIntegration) Name() string {
	return "IgnoreErrors"
}

func (iei *ignoreErrorsIntegration) SetupOnce(client *Client) {
	iei.ignoreErrors = transformStringsIntoRegexps(client.options.IgnoreErrors)
	client.AddEventProcessor(iei.processor)
}

func (iei *ignoreErrorsIntegration) processor(event *Event, _ *EventHint) *Event {
	suspects := getIgnoreErrorsSuspects(event)

	for _, suspect := range suspects {
		for _, pattern := range iei.ignoreErrors {
			if pattern.Match([]byte(suspect)) || strings.Contains(suspect, pattern.String()) {
				debuglog.Printf("Event dropped due to being matched by `IgnoreErrors` option."+
					"| Value matched: %s | Filter used: %s", suspect, pattern)
				return nil
			}
		}
	}

	return event
}

func transformStringsIntoRegexps(strings []string) []*regexp.Regexp {
	var exprs []*regexp.Regexp

	for _, s := range strings {
		r, err := regexp.Compile(s)
		if err == nil {
			exprs = append(exprs, r)
		}
	}

	return exprs
}

func getIgnoreErrorsSuspects(event *Event) []string {
	suspects := []string{}

	if event.Message != "" {
		suspects = append(suspects, event.Message)
	}

	for _, ex := range event.Exception {
		suspects = append(suspects, ex.Type, ex.Value)
	}

	return suspects
}

// ================================
// Ignore Transactions Integration
// ================================

type ignoreTransactionsIntegration struct {
	ignoreTransactions []*regexp.Regexp
}

func (iei *ignoreTransactionsIntegration) Name() string {
	return "IgnoreTransactions"
}

func (iei *ignoreTransactionsIntegration) SetupOnce(client *Client) {
	iei.ignoreTransactions = transformStringsIntoRegexps(client.options.IgnoreTransactions)
	client.transactionProcessors = append(client.transactionProcessors, iei.processor)
}

func (iei *ignoreTransactionsIntegration) processor(tx *Transaction) *Transaction {
	suspect := tx.Name
	if suspect == "" {
		return tx
	}

	for _, pattern := range iei.ignoreTransactions {
		if pattern.Match([]byte(suspect)) || strings.Contains(suspect, pattern.String()) {
			debuglog.Printf("Transaction dropped due to being matched by `IgnoreTransactions` option."+
				"| Value matched: %s | Filter used: %s", suspect, pattern)
			return nil
		}
	}

	return tx
}

// ================================
// Global Tags Integration
// ================================

const envTagsPrefix = "SENTRY_TAGS_"

type globalTagsIntegration struct {
	tags    map[string]string
	envTags map[string]string
}

func (ti *globalTagsIntegration) Name() string {
	return "GlobalTags"
}

func (ti *globalTagsIntegration) SetupOnce(client *Client) {
	ti.tags = make(map[string]string, len(client.options.Tags))
	maps.Copy(ti.tags, client.options.Tags)

	ti.envTags = loadEnvTags()

	client.AddEventProcessor(func(event *Event, _ *EventHint) *Event {
		event.Tags = ti.merge(event.Tags)
		return event
	})
	client.transactionProcessors = append(client.transactionProcessors, func(tx *Transaction) *Transaction {
		tx.Tags = ti.merge(tx.Tags)
		return tx
	})
}

// merge adds the global tags that tags does not set. Tags from the client
// options take precedence over tags from the environment.
func (ti *globalTagsIntegration) merge(tags map[string]string) map[string]string {
	return mergeTags(ti.envTags, mergeTags(ti.tags, tags))
}

func loadEnvTags() map[string]string {
	tags := map[string]string{}
	for _, pair := range os.Environ() {
		parts := strings.Split(pair, "=")
		if !strings.HasPrefix(parts[0], envTagsPrefix) {
			continue
		}
		tag := strings.TrimPrefix(parts[0], envTagsPrefix)
		tags[tag] = parts[1]
	}
	return tags
}
