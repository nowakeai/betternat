package buildinfo

import (
	"fmt"
	"runtime"
)

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

type Info struct {
	Program string `json:"program"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	Go      string `json:"go"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}

func Current(program string) Info {
	return Info{
		Program: program,
		Version: Version,
		Commit:  Commit,
		Date:    Date,
		Go:      runtime.Version(),
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
	}
}

func (i Info) String() string {
	return fmt.Sprintf("%s version=%s commit=%s date=%s go=%s os=%s arch=%s", i.Program, i.Version, i.Commit, i.Date, i.Go, i.OS, i.Arch)
}
