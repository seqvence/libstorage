package drivers

import (
	"github.com/akutz/gofig"
)

// Driver is the base interface for a libStorage driver.
type Driver interface {

	// Name returns the name of the driver
	Name() string

	// Init initializes the driver.
	Init(config gofig.Config) error
}
