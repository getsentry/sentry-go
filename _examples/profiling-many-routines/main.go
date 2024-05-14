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
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"time"

	"github.com/getsentry/sentry-go"
)

const numRoutines = 5000
const restAmount = 20000
const workAmount = 10000

func main() {
	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()

	err := sentry.Init(sentry.ClientOptions{
		// Either set your DSN here or set the SENTRY_DSN environment variable.
		Dsn: "", // "https://3c3fd18b3fd44566aeab11385f391a48@o447951.ingest.us.sentry.io/5774600",
		// Enable printing of SDK debug messages.
		// Useful when getting started or trying to figure something out.
		Debug:              false,
		EnableTracing:      true,
		TracesSampleRate:   1.0,
		ProfilesSampleRate: 1.00,
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
