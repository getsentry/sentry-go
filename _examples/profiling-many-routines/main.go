//	go run main.go
//
// To actually report events to Sentry, set the DSN either by editing the
// appropriate line below or setting the environment variable SENTRY_DSN to
// match the DSN of your Sentry project.
package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"time"

	"github.com/getsentry/sentry-go"
)

const numRoutines = 5000
const restAmount = 20000
const workAmount = 10000

func main() {
	err := sentry.Init(sentry.ClientOptions{
		// Either set your DSN here or set the SENTRY_DSN environment variable.
		Dsn: "",
		// Enable printing of SDK debug messages.
		// Useful when getting started or trying to figure something out.
		Debug:              true,
		EnableTracing:      true,
		TracesSampleRate:   1.0,
		ProfilesSampleRate: 0.05,
	})

	if err != nil {
		log.Fatalf("sentry.Init: %s", err)
	}

	for i := 0; i < numRoutines; i++ {
		go func(id int) {
			for j := 0; true; j++ {
				tx := sentry.StartTransaction(context.Background(), fmt.Sprintf("Routine %d, run %d", id, j))
				_ = findPrimeNumber(workAmount)
				tx.Finish()
				time.Sleep(time.Duration(100+rand.Intn(restAmount)) * time.Millisecond)
			}
		}(i)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
}

func findPrimeNumber(n int) int {
	count := 0
	a := 2
	for count < n {
		b := 2
		prime := true // to check if found a prime
		for b*b <= a {
			if a%b == 0 {
				prime = false
				break
			}
			b++
		}
		if prime {
			count++
		}
		a++
	}
	return a - 1
}
