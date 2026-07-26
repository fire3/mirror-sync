package taskctrl

import "errors"

// ErrCancelled is returned by Check() when the task has been cancelled.
var ErrCancelled = errors.New("task cancelled")

// Controller implements pause/resume/cancel for long-running tasks.
type Controller struct {
	state       string // "running", "paused", "cancelled"
	pauseResume chan struct{}
}

// New creates a new Controller in the running state.
func New() *Controller {
	return &Controller{
		state:       "running",
		pauseResume: make(chan struct{}, 1),
	}
}

// Pause suspends the task.
func (c *Controller) Pause() {
	if c.state == "running" {
		c.state = "paused"
	}
}

// Resume continues a paused task.
func (c *Controller) Resume() {
	if c.state == "paused" {
		c.state = "running"
		select {
		case c.pauseResume <- struct{}{}:
		default:
		}
	}
}

// Cancel cancels the task permanently.
func (c *Controller) Cancel() {
	c.state = "cancelled"
	select {
	case c.pauseResume <- struct{}{}:
	default:
	}
}

// IsPaused returns true if the task is paused.
func (c *Controller) IsPaused() bool {
	return c.state == "paused"
}

// IsCancelled returns true if the task has been cancelled.
func (c *Controller) IsCancelled() bool {
	return c.state == "cancelled"
}

// GetState returns the current state.
func (c *Controller) GetState() string {
	return c.state
}

// Check blocks if paused and returns ErrCancelled if cancelled.
func (c *Controller) Check() error {
	if c.state == "cancelled" {
		return ErrCancelled
	}
	if c.state == "paused" {
		<-c.pauseResume
		if c.state == "cancelled" {
			return ErrCancelled
		}
	}
	return nil
}
