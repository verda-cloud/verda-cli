package util

import "github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

// InstanceBillableUnits returns the number of billable units for an instance.
// GPU instances are billed per GPU; CPU instances are billed per vCPU.
func InstanceBillableUnits(inst *verda.Instance) int {
	if inst.GPU.NumberOfGPUs > 0 {
		return inst.GPU.NumberOfGPUs
	}
	if inst.CPU.NumberOfCores > 0 {
		return inst.CPU.NumberOfCores
	}
	return 1
}

// InstanceTypeBillableUnits returns the number of billable units for an instance type.
// GPU types are billed per GPU; CPU types are billed per vCPU.
func InstanceTypeBillableUnits(t *verda.InstanceTypeInfo) int {
	if t.GPU.NumberOfGPUs > 0 {
		return t.GPU.NumberOfGPUs
	}
	if t.CPU.NumberOfCores > 0 {
		return t.CPU.NumberOfCores
	}
	return 1
}

// InstanceTotalHourlyCost returns the total hourly cost for an instance.
//
// The API field PricePerHour is the per-unit price:
//   - GPU instances: price per GPU (multiply by GPU count)
//   - CPU instances: price per vCPU (multiply by vCPU count)
func InstanceTotalHourlyCost(inst *verda.Instance) float64 {
	pricePerUnit := float64(inst.PricePerHour)
	return pricePerUnit * float64(InstanceBillableUnits(inst))
}
