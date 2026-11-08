package health

import (
	"math"
	"strconv"
	"strings"
)

const (
	pressureDirectory        = "pressure"
	pressureMinimumFields    = 4
	pressureWarningThreshold = 20
)

type pressureResource string

const (
	pressureCPU    pressureResource = "cpu"
	pressureMemory pressureResource = "memory"
	pressureIO     pressureResource = "io"
)

type pressureRecord string

const (
	pressureSome pressureRecord = "some"
	pressureFull pressureRecord = "full"
)

type pressureField string

const pressureAvg10 pressureField = "avg10"

type pressureDescriptor struct {
	resource  pressureResource
	findingID string
}

var pressureResources = [...]pressureDescriptor{
	{pressureCPU, coreFindingPressureCPU}, {pressureMemory, coreFindingPressureMemory}, {pressureIO, coreFindingPressureIO},
}

func (d pressureDescriptor) name() string {
	return string(d.resource)
}

type pressureConfig struct {
	warningThreshold float64
	minimumFields    int
}
type pressureResult struct {
	valid, high bool
	evidence    []string
}

var defaultPressureConfig = pressureConfig{warningThreshold: pressureWarningThreshold, minimumFields: pressureMinimumFields}

func parsePressure(input string) pressureResult {
	return parsePressureWithConfig(input, defaultPressureConfig)
}

func parsePressureWithConfig(input string, config pressureConfig) pressureResult {
	input = strings.TrimSpace(input)
	result := pressureResult{valid: true, evidence: make([]string, 0, 2)}
	if input == "" {
		return pressureResult{}
	}
	for line := range strings.Lines(input) {
		line = strings.TrimSuffix(line, "\n")
		fields := strings.FieldsSeq(line)
		count := 0
		found := false
		lineHigh := false
		recordValid := true
		var normalized strings.Builder
		normalized.Grow(len(line))
		for field := range fields {
			if count > 0 {
				normalized.WriteByte(' ')
			}
			normalized.WriteString(field)
			if count == 0 {
				if pressureRecord(field) != pressureSome && pressureRecord(field) != pressureFull {
					result.valid = false
					recordValid = false
					break
				}
			} else if key, value, ok := strings.Cut(field, "="); ok && pressureField(key) == pressureAvg10 {
				n, err := strconv.ParseFloat(value, 64)
				if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 100 {
					result.valid = false
				} else if n >= config.warningThreshold {
					lineHigh = true
				}
				found = true
			}
			count++
		}
		if count < config.minimumFields || !found {
			result.valid = false
		}
		if count < config.minimumFields || !recordValid {
			break
		}
		result.high = result.high || lineHigh
		result.evidence = append(result.evidence, normalized.String())
	}
	if len(result.evidence) == 0 {
		result.valid = false
	}
	return result
}
