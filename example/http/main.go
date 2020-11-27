// This is an example web server to demonstrate how to instrument web servers
// with Sentry.
//
// Try it by running:
//
// 	go run main.go
//
// To actually report events to Sentry, set the DSN either by editing the
// appropriate line below or setting the environment variable SENTRY_DSN to
// match the DSN of your Sentry project.
package main

import (
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"html/template"
	"image"
	"image/png"
	"log"
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
)

var addr = flag.String("addr", "127.0.0.1:3000", "bind address")

func main() {
	flag.Parse()
	configureLoggers()
	// The helper run function does not call log.Fatal, otherwise deferred
	// function calls would not be executed when the program exits.
	log.Fatal(run())
}

// run runs a web server. As with http.ListenAndServe, the returned error is
// always non-nil.
func run() error {
	err := sentry.Init(sentry.ClientOptions{
		// Either set your DSN here or set the SENTRY_DSN environment variable.
		Dsn: "",
		// Enable printing of SDK debug messages.
		// Useful when getting started or trying to figure something out.
		Debug: true,
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			// Here you can inspect/modify events before they are sent.
			// Returning nil drops the event.
			log.Printf("BeforeSend event [%s]", event.EventID)
			return event
		},
	})
	if err != nil {
		return err
	}
	// Flush buffered events before the program terminates.
	// Set the timeout to the maximum duration the program can afford to wait.
	defer sentry.Flush(2 * time.Second)

	// Main HTTP handler, renders an HTML page with a random image.
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			// Use GetHubFromContext to get a hub associated with the current
			// request. Hubs provide data isolation, such that tags, breadcrumbs
			// and other attributes are never mixed up across requests.
			hub := sentry.GetHubFromContext(r.Context())
			hub.Scope().SetTag("url", r.URL.Path)
			hub.CaptureMessage("Page Not Found")
			http.NotFound(w, r)
			return
		}

		err := t.Execute(w, time.Now().UnixNano())
		if err != nil {
			log.Printf("[%s] %s", r.URL.Path, err)
			return
		}
	})

	// HTTP handler for the random image.
	http.HandleFunc("/random.png", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		var cancel context.CancelFunc
		if timeout, err := time.ParseDuration(r.URL.Query().Get("timeout")); err == nil {
			log.Printf("Rendering random image with timeout = %v", timeout)
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}

		q := r.URL.Query().Get("q")
		img := NewImage(ctx, 128, 128, []byte(q))

		err := png.Encode(w, img)
		if err != nil {
			log.Printf("[%s] %s", r.URL.Path, err)
			hub := sentry.GetHubFromContext(ctx)
			hub.CaptureException(err)
			code := http.StatusInternalServerError
			http.Error(w, http.StatusText(code), code)
			return
		}
	})

	http.HandleFunc("/panic/", func(w http.ResponseWriter, r *http.Request) {
		var s []int
		fmt.Fprint(w, s[42]) // this line will panic
	})

	log.Printf("Serving http://%s", *addr)

	// Wrap the default mux with Sentry to capture panics and report errors.
	//
	// Alternatively, you can also wrap individual handlers if you need to use
	// different options for different parts of your app.
	handler := sentryhttp.New(sentryhttp.Options{}).Handle(http.DefaultServeMux)
	return http.ListenAndServe(*addr, handler)
}

var t = template.Must(template.New("").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<title>Random Image</title>
<style>
img {
	width: 128px;
	height: 128px;
}
</style>
<base target="_blank">
</head>
<body>
<h1>Random Image</h1>
<img src="/random.png?q={{.}}" alt="Random Image">
<h2>Click one of these links to send an event to Sentry</h2>
<ul>
<li><a href="/random.png?q={{.}}&timeout=20ms">Open random image and abort if it takes longer than 20ms</a></li>
<li><a href="/404">Trigger 404 not found error</a></li>
<li><a href="/panic/">Trigger server-side panic</a></li>
</ul>
</body>
</html>`))

// NewImage returns a random image based on seed, with the given width and
// height.
func NewImage(ctx context.Context, width, height int, seed []byte) image.Image {
	b := sha256.Sum256(seed)

	img := image.NewGray(image.Rect(0, 0, width, height))

	for i := 0; i < len(img.Pix); i += len(b) {
		select {
		case <-ctx.Done():
			// Context canceled, abort image generation.
			// Spot the bug: the returned image cannot be encoded as PNG and
			// will cause an error that will be reported to Sentry.
			return img.SubImage(image.Rect(0, 0, 0, 0))
		default:
		}
		p := b[:]
		for j := 0; j < i; j++ {
			tmp := sha256.Sum256(p)
			p = tmp[:]
		}
		copy(img.Pix[i:], p)
	}
	return img
}

// configureLoggers configures the standard logger and the logger used by the
// Sentry SDK.
//
// The only reason to change logger configuration in this example is aesthetics.
func configureLoggers() {
	logFlags := log.Ldate | log.Ltime
	sentry.Logger.SetPrefix("[sentry sdk]   ")
	sentry.Logger.SetFlags(logFlags)
	log.SetPrefix("[http example] ")
	log.SetFlags(logFlags)
}
