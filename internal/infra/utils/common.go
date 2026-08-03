package utils

import (
	"log"
	"runtime"
	"time"
)

// FancyHandleError this logs the function name as well.
func FancyHandleError(err error) (b bool) {
	if err != nil {
		// notice that we're using 1, so it will actually log the where
		// the Error happened, 0 = this function, we don't want that.
		pc, filename, line, _ := runtime.Caller(1)

		log.Printf("[Error] in %s[%s:%d] %v", runtime.FuncForPC(pc).Name(), filename, line, err)

		b = true
	}
	return
}

type Errors struct {
	Errors []Errorlog `json:"errors"`
}
type Errorlog struct {
	Error string    `json:"error" format:"string"`
	Time  time.Time `json:"refresh_time" format:"time"`
} // @name Errorlog

type TypeVersion struct {
	OSVersion                string   `json:"os_version,omitempty"`
	KernelVersion            string   `json:"kernel_version,omitempty"`
	CockpitVersion           string   `json:"cockpit_version,omitempty"`
	MoldVersion              string   `json:"mold_version,omitempty"`
	GlueVersion              string   `json:"glue_version,omitempty"`
	AbleStackPackageVersions []string `json:"ablestack_package_versions,omitempty"`
	Debug                    bool     `json:"debug,omitempty"`
}
