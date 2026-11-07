package sysmon

// LoadAverage is the 1-minute load average (0 when unknown). Darwin and Linux read the kernel value directly; other
// systems have none.
func LoadAverage() float64 {
	if v := loadAverageOS(); v != nil {
		return *v
	}
	return 0
}
