package ctxmgr

import "sync"

// Calibrator adjusts local token estimates using provider-reported prompt_tokens.
// Ratio is actual/estimated, smoothed with EMA and clamped to a safe range.
type Calibrator struct {
	mu      sync.Mutex
	ratio   float64
	samples int
	// last holds the most recent observation for inspectors.
	lastEstimate int
	lastActual   int
}

// NewCalibrator returns a calibrator starting at ratio 1.0.
func NewCalibrator() *Calibrator {
	return &Calibrator{ratio: 1.0}
}

// Ratio returns the current actual/estimate multiplier.
func (c *Calibrator) Ratio() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ratio
}

// Samples returns how many observations have been folded in.
func (c *Calibrator) Samples() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.samples
}

// Last returns the most recent (estimated, actual) pair.
func (c *Calibrator) Last() (estimate, actual int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastEstimate, c.lastActual
}

// Observe folds one provider usage sample into the ratio.
// estimated is the local snapshot total (messages + tools); actual is usage.prompt_tokens.
func (c *Calibrator) Observe(estimated, actual int) {
	if estimated <= 0 || actual <= 0 {
		return
	}
	r := float64(actual) / float64(estimated)

	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastEstimate = estimated
	c.lastActual = actual
	if c.samples == 0 {
		c.ratio = r
	} else {
		// EMA: weight new sample moderately so one outlier cannot dominate.
		c.ratio = 0.25*r + 0.75*c.ratio
	}
	c.samples++
	if c.ratio < 0.5 {
		c.ratio = 0.5
	}
	if c.ratio > 2.5 {
		c.ratio = 2.5
	}
}

// Scale multiplies a base heuristic estimate by the learned ratio.
func (c *Calibrator) Scale(n int) int {
	if c == nil {
		return n
	}
	c.mu.Lock()
	r := c.ratio
	c.mu.Unlock()
	if r == 1.0 {
		return n
	}
	v := int(float64(n)*r + 0.5)
	if v < 1 && n > 0 {
		v = 1
	}
	return v
}
