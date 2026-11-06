package sandbox

import "errors"

const developmentValidationPolicy = "development-validator-v1"
const developmentValidationSupervisor = "development-validator-supervisor-v1"

// VerifyValidationEntry is the second, child pre-parse gate. Production rechecks
// the independent kernel controllers. Development requires the trusted sealed
// launch descriptor AND the actual child limits; it never reads an opt-in env.
func VerifyValidationEntry(memoryMB, cpuPercent int, policy string) error {
	if policy == developmentValidationPolicy {
		if memoryMB != 128 || cpuPercent != 50 {
			return errors.New("invalid development validator budget")
		}
		return verifyDevelopmentValidationEntry()
	}
	return VerifyValidationResources(memoryMB, cpuPercent, policy)
}
