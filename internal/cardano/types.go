package cardano

import "time"

type ClientLeaderLogsResponse struct {
	Status           string         `json:"status"`
	ErrorMessage     string         `json:"errorMessage,omitempty"`
	Epoch            int            `json:"epoch,omitempty"`
	EpochNonce       string         `json:"epochNonce,omitempty"`
	Consensus        string         `json:"consensus,omitempty"`
	EpochSlots       int            `json:"epochSlots,omitempty"`
	EpochSlotsIdeal  float64        `json:"epochSlotsIdeal,omitempty"`
	MaxPerformance   float64        `json:"maxPerformance,omitempty"`
	PoolID           string         `json:"PoolID,omitempty"` //nolint:tagliatelle
	Sigma            float64        `json:"sigma,omitempty"`
	ActiveStake      int            `json:"activeStake,omitempty"`
	TotalActiveStake int            `json:"totalActiveStake,omitempty"`
	AssignedSlots    []SlotSchedule `json:"assignedSlots,omitempty"`
}

type SlotSchedule struct {
	No          int       `json:"no,omitempty"`
	Slot        int       `json:"slot,omitempty"`
	SlotInEpoch int       `json:"slotInEpoch,omitempty"`
	At          time.Time `json:"at,omitempty"`
}

type ClientQueryStakeSnapshotResponse struct {
	Pools map[string]PoolStakeInfo `json:"pools,omitempty"`
	Total TotalStakeInfo           `json:"total,omitempty"`
}

type PoolStakeInfo struct {
	StakeGo   int `json:"stakeGo,omitempty"`
	StakeMark int `json:"stakeMark,omitempty"`
	StakeSet  int `json:"stakeSet,omitempty"`
}

type TotalStakeInfo struct {
	StakeGo   int `json:"stakeGo,omitempty"`
	StakeMark int `json:"stakeMark,omitempty"`
	StakeSet  int `json:"stakeSet,omitempty"`
}
