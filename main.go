package main

import (
	"context"
	"fmt"
	"time"

	eth2client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/api"
	eth2http "github.com/attestantio/go-eth2-client/http"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/rs/zerolog/log"
)

const (
	SLOTS_PER_EPOCH = 32
)

func EpochLowestSlot(epoch phase0.Epoch) phase0.Slot {
	return phase0.Slot(epoch * SLOTS_PER_EPOCH)
}

func EpochHighestSlot(epoch phase0.Epoch) phase0.Slot {
	return phase0.Slot(((epoch + 1) * SLOTS_PER_EPOCH) - 1)
}

func GetBlock(service eth2client.Service, slot phase0.Slot) (*electra.SignedBeaconBlock, error) {
	provider := service.(eth2client.SignedBeaconBlockProvider)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(time.Minute*1))
	defer cancel()

	resp, err := provider.SignedBeaconBlock(ctx, &api.SignedBeaconBlockOpts{
		Block: fmt.Sprintf("%v", slot),
	})

	if err != nil {
		return nil, err
	}

	if resp == nil {
		// Missed slot
		return nil, nil
	}

	return resp.Data.Electra, err
}

func ListEpochBlocks(service eth2client.Service, epoch phase0.Epoch) (map[phase0.Slot]*electra.SignedBeaconBlock, error) {
	result := make(map[phase0.Slot]*electra.SignedBeaconBlock, SLOTS_PER_EPOCH)
	low := EpochLowestSlot(epoch)
	high := EpochHighestSlot(epoch)
	for slot := low; slot <= high; slot++ {
		block, err := GetBlock(service, phase0.Slot(slot))

		if err != nil {
			log.Error().Err(err)
			continue
		}

		if block == nil {
			// Missed slot
			continue
		}

		result[slot] = block
	}
	return result, nil
}

func GetBeaconCommitees(ctx context.Context, service eth2client.Service, epoch phase0.Epoch) (map[phase0.CommitteeIndex][]phase0.ValidatorIndex, error) {
	provider := service.(eth2client.BeaconCommitteesProvider)
	resp, err := provider.BeaconCommittees(ctx, &api.BeaconCommitteesOpts{
		State: fmt.Sprintf("%d", EpochLowestSlot(epoch)),
		Epoch: &epoch,
	})
	if err != nil {
		return nil, err
	}

	result := make(map[phase0.CommitteeIndex][]phase0.ValidatorIndex)
	for _, committee := range resp.Data {
		result[committee.Index] = committee.Validators
	}

	return result, err
}

func main() {
	beacon_api_url := "..."       // Put your beacon node URL (http) here
	epoch := phase0.Epoch(...) // Grab the latest finalized epoch number e.g. from https://beaconcha.in/

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(time.Minute*1))
	defer cancel()
	service, err := eth2http.New(ctx, eth2http.WithAddress(beacon_api_url), eth2http.WithTimeout(time.Minute))
	if err != nil {
		log.Fatal().Msg("failed creating service")
	}

	var epochBlocks map[phase0.Slot]*electra.SignedBeaconBlock
	epochBlocks, err = ListEpochBlocks(service, phase0.Epoch(epoch))
	if err != nil {
		log.Fatal().Msg("failed listing epoch blocks")
	}

	committees := make(map[phase0.CommitteeIndex][]phase0.ValidatorIndex)
	committees, err = GetBeaconCommitees(ctx, service, phase0.Epoch(epoch))

	fmt.Printf("EpochLowestSlot(epoch): %v\n", EpochLowestSlot(epoch))
	fmt.Printf("EpochHighestSlot(epoch): %v\n", EpochHighestSlot(epoch))

	for _, block := range epochBlocks {
		for _, attestation := range block.Message.Body.Attestations {
			committeesLen := 0
			for committeeIndex := range attestation.CommitteeBits.BitIndices() {
				committeesLen += len(committees[phase0.CommitteeIndex(committeeIndex)])
			}
			if attestation.AggregationBits.Len() != uint64(committeesLen) {
				log.Error().Msgf("length mismatch (slot=%v): computed=%v actual=%v", block.Message.Slot, committeesLen, attestation.AggregationBits.Len())
			}
		}
	}
}
