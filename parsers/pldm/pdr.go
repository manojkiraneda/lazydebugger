package pldm

import (
	"encoding/json"
	"fmt"
	"sync"
)

// StateSensorPDR is the subset of a pldmtool "State Sensor PDR" JSON record
// that we need for display. All fields are populated by LoadPDRIndex.
type StateSensorPDR struct {
	SensorID             uint16
	EntityType           string
	EntityInstanceNumber int
	ContainerID          int
	CompositeSensorCount int
	// Per-composite-sensor slot (index 0..CompositeSensorCount-1)
	StateSetIDs    []string   // stateSetID[N] raw string, e.g. "Operational Fault Status(10)"
	PossibleStates [][]string // possibleStates[N] list of state strings
}

// pdrIndex is the in-memory lookup table, rebuilt by LoadPDRIndex.
var (
	pdrMu    sync.RWMutex
	pdrIndex map[uint16]*StateSensorPDR // keyed by SensorID
)

// LoadPDRIndex parses the raw JSON array produced by
// "pldmtool platform getpdr -a" and builds an in-memory index of State Sensor
// PDRs keyed by SensorID.  Safe to call from any goroutine; the update is
// atomic under pdrMu.
func LoadPDRIndex(rawJSON []byte) error {
	var records []map[string]json.RawMessage
	if err := json.Unmarshal(rawJSON, &records); err != nil {
		return fmt.Errorf("pdr: unmarshal: %w", err)
	}

	idx := make(map[uint16]*StateSensorPDR, 256)
	for _, rec := range records {
		// Only care about State Sensor PDRs.
		var pdrType string
		if err := json.Unmarshal(rec["PDRType"], &pdrType); err != nil {
			continue
		}
		if pdrType != "State Sensor PDR" {
			continue
		}

		var sid uint16
		if err := json.Unmarshal(rec["sensorID"], &sid); err != nil {
			continue
		}

		pdr := &StateSensorPDR{SensorID: sid}

		if v, ok := rec["entityType"]; ok {
			json.Unmarshal(v, &pdr.EntityType) //nolint:errcheck
		}
		if v, ok := rec["entityInstanceNumber"]; ok {
			json.Unmarshal(v, &pdr.EntityInstanceNumber) //nolint:errcheck
		}
		if v, ok := rec["containerID"]; ok {
			json.Unmarshal(v, &pdr.ContainerID) //nolint:errcheck
		}
		if v, ok := rec["compositeSensorCount"]; ok {
			json.Unmarshal(v, &pdr.CompositeSensorCount) //nolint:errcheck
		}

		for i := 0; i < pdr.CompositeSensorCount; i++ {
			setKey := fmt.Sprintf("stateSetID[%d]", i)
			stateKey := fmt.Sprintf("possibleStates[%d]", i)

			var setID string
			if v, ok := rec[setKey]; ok {
				json.Unmarshal(v, &setID) //nolint:errcheck
			}
			pdr.StateSetIDs = append(pdr.StateSetIDs, setID)

			var states []string
			if v, ok := rec[stateKey]; ok {
				json.Unmarshal(v, &states) //nolint:errcheck
			}
			pdr.PossibleStates = append(pdr.PossibleStates, states)
		}

		idx[sid] = pdr
	}

	pdrMu.Lock()
	pdrIndex = idx
	pdrMu.Unlock()
	return nil
}

// LookupStateSensorPDR returns the State Sensor PDR for the given sensorID,
// or nil when the index is empty or the sensor is not found.
func LookupStateSensorPDR(sensorID uint16) *StateSensorPDR {
	pdrMu.RLock()
	defer pdrMu.RUnlock()
	if pdrIndex == nil {
		return nil
	}
	return pdrIndex[sensorID]
}
