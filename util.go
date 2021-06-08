package sentry

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

func uuid() string {
	id := make([]byte, 16)
	// Prefer rand.Read over rand.Reader, see https://go-review.googlesource.com/c/go/+/272326/.
	_, _ = rand.Read(id)
	id[6] &= 0x0F // clear version
	id[6] |= 0x40 // set version to 4 (random uuid)
	id[8] &= 0x3F // clear variant
	id[8] |= 0x80 // set to IETF variant
	return hex.EncodeToString(id)
}

func fileExists(fileName string) bool {
	_, err := os.Stat(fileName)
	return err == nil
}

// monotonicTimeSince replaces uses of time.Now() to take into account the
// monotonic clock reading stored in start, such that duration = end - start is
// unaffected by changes in the system wall clock.
func monotonicTimeSince(start time.Time) (end time.Time) {
	return start.Add(time.Since(start))
}

//nolint: deadcode, unused
func prettyPrint(data interface{}) {
	dbg, _ := json.MarshalIndent(data, "", "  ")
	fmt.Println(string(dbg))
}

// attempts to guess a default release.
func defaultRelease() string {
	// Search environment variables (EV) known to hold release info.
	envs := []string{
		"SENTRY_RELEASE",
		"HEROKU_SLUG_COMMIT",
		"SOURCE_VERSION",
		"CODEBUILD_RESOLVED_SOURCE_VERSION",
		"CIRCLE_SHA1",
		"GAE_DEPLOYMENT_ID",
		"GITHUB_SHA",             // GitHub Actions - https://help.github.com/en/actions
		"COMMIT_REF",             // Netlify - https://docs.netlify.com/
		"VERCEL_GIT_COMMIT_SHA",  // Vercel - https://vercel.com/
		"ZEIT_GITHUB_COMMIT_SHA", // Zeit (now known as Vercel)
		"ZEIT_GITLAB_COMMIT_SHA",
		"ZEIT_BITBUCKET_COMMIT_SHA"}
	for _, e := range envs {
		if val := os.Getenv(e); val != "" {
			return val // Stop at first non-empty variable.
		}
	}

	// No EV's, attempt to get the last commit hash with git.
	var stdout bytes.Buffer
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Stdout = &stdout
	err := cmd.Run()
	if err != nil {
		Logger.Println("Failed attempt to run git rev-parse.")
	} else {
		shastr := strings.TrimSpace(stdout.String())
		if len(shastr) == 40 { // sha1 hash length
			return shastr
		}
	}

	// Not able to find a release name at all.
	return ""
}
