package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/effectgateway"
	"github.com/camem2442/keycli/internal/rotation"
	"golang.org/x/sys/unix"
)

const maxRotationInputBytes = 64 << 10

var errRotationInput = errors.New("invalid rotation plan input")

func buildRotationPlan(path string) (rotation.PreparationPlan, error) {
	data, err := readRotationInput(path)
	if err != nil {
		return rotation.PreparationPlan{}, err
	}

	var version int
	var old, next json.RawMessage
	var ids []string
	if !effectgateway.DecodePrivateConfig(data, map[string]any{
		"v": &version, "old": &old, "new": &next, "required_consumers": &ids,
	}) || version != 1 {
		return rotation.PreparationPlan{}, errRotationInput
	}

	var oldGeneration, newGeneration rotation.Generation
	if !decodeRotationGeneration(old, &oldGeneration) || !decodeRotationGeneration(next, &newGeneration) {
		return rotation.PreparationPlan{}, errRotationInput
	}
	consumers := make([]rotation.Consumer, len(ids))
	for i, id := range ids {
		consumers[i] = rotation.Consumer{ID: id, Required: true}
	}
	plan, err := rotation.NewPreparationPlan(oldGeneration, newGeneration, consumers)
	if err != nil {
		return rotation.PreparationPlan{}, errRotationInput
	}
	return plan, nil
}

func decodeRotationGeneration(data []byte, generation *rotation.Generation) bool {
	return effectgateway.DecodePrivateConfig(data, map[string]any{
		"alias": &generation.Alias, "kind": &generation.Kind,
		"provider": &generation.Provider, "target": &generation.Target,
		"number": &generation.Number,
	})
}

func readRotationInput(path string) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errors.New("cannot open rotation plan input")
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()

	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		return nil, errors.New("cannot inspect rotation plan input")
	}
	mode := info.Mode & 07777
	if info.Mode&unix.S_IFMT != unix.S_IFREG || info.Uid != uint32(os.Geteuid()) ||
		info.Nlink != 1 || (mode != 0600 && mode != 0400) {
		return nil, errors.New("rotation plan input must be an owner-only regular file with one link and mode 0600 or 0400")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxRotationInputBytes+1))
	if err != nil {
		return nil, errors.New("cannot read rotation plan input")
	}
	if len(data) > maxRotationInputBytes {
		return nil, errRotationInput
	}
	return data, nil
}

// RotationStatus projects the local journal and unverified session metadata.
type RotationStatus struct {
	Version             int                   `json:"v"`
	PlanDigest          string                `json:"plan_digest"`
	State               rotation.SessionState `json:"state"`
	RequiredConsumers   []string              `json:"required_consumers"`
	MissingConsumers    []string              `json:"missing_consumers"`
	UnverifiedConsumers []string              `json:"unverified_consumers"`
	Grace               *rotationGrace        `json:"grace,omitempty"`
}

type rotationGrace struct {
	ActivatedAt time.Time `json:"activated_at"`
	GraceUntil  time.Time `json:"grace_until"`
	Verified    bool      `json:"verified"`
}

func loadRotationStatus(ctx context.Context, journalRoot, planID string, now time.Time) (RotationStatus, error) {
	store := journal.Store{Root: journalRoot}
	snapshot, err := rotation.LoadSession(store, planID)
	if err != nil {
		return RotationStatus{}, err
	}
	session := snapshot.Session
	state, err := session.State(ctx, store, now, rotation.SessionVerifiers{})
	if err != nil {
		return RotationStatus{}, err
	}

	status := RotationStatus{
		Version:             1,
		PlanDigest:          session.Plan.ID,
		State:               state,
		RequiredConsumers:   make([]string, 0, len(session.Plan.Consumers)),
		MissingConsumers:    make([]string, 0),
		UnverifiedConsumers: make([]string, 0),
	}
	hasEvidence := make(map[string]bool, len(session.Consumers))
	for _, evidence := range session.Consumers {
		hasEvidence[evidence.ConsumerID] = true
	}
	for _, consumer := range session.Plan.Consumers {
		status.RequiredConsumers = append(status.RequiredConsumers, consumer.ID)
		if hasEvidence[consumer.ID] {
			status.UnverifiedConsumers = append(status.UnverifiedConsumers, consumer.ID)
		} else {
			status.MissingConsumers = append(status.MissingConsumers, consumer.ID)
		}
	}
	if session.Activation != nil {
		status.Grace = &rotationGrace{
			ActivatedAt: session.Activation.ActivatedAt.UTC(),
			GraceUntil:  session.Activation.GraceUntil.UTC(),
			Verified:    false,
		}
	}
	return status, nil
}
