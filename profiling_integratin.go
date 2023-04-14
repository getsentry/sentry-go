package sentry

type profilingIntegration struct{}

func (ei *profilingIntegration) Name() string {
	return "Profiling"
}

func (ei *profilingIntegration) SetupOnce(client *Client) {
	// TODO implement - attach to StartSpan() and Finish() to capture profiling data.
}

