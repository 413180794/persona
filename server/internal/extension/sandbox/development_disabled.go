//go:build !persona_dev_resources || !linux

package sandbox

import "io"

const developmentResourcesBuilt = false

func runDevelopmentExec([]string, io.Writer) int          { return 2 }
func runDevelopmentChild([]string, string, io.Writer) int { return 2 }
