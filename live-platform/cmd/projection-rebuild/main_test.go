package main

import (
	"context"
	"testing"
)

type fakeIdentityRebuilder struct{ calls int }

func (f *fakeIdentityRebuilder) RebuildProjectionBatch(_ context.Context, after int64, _ int) (int64, int, error) {
	f.calls++
	if after == 0 {
		return 4, 2, nil
	}
	return after, 0, nil
}

type fakeRoomRebuilder struct{ roomCalls, governanceCalls int }

func (f *fakeRoomRebuilder) RebuildRoomProjectionBatch(_ context.Context, after int64, _ int) (int64, int, error) {
	f.roomCalls++
	if after == 0 {
		return 9, 1, nil
	}
	return after, 0, nil
}
func (f *fakeRoomRebuilder) RebuildGovernanceProjectionBatch(_ context.Context, roomID, userID int64, _ int) (int64, int64, int, error) {
	f.governanceCalls++
	if roomID == 0 && userID == 0 {
		return 9, 12, 3, nil
	}
	return roomID, userID, 0, nil
}

func TestRebuildPagesEveryProjection(t *testing.T) {
	identities := &fakeIdentityRebuilder{}
	rooms := &fakeRoomRebuilder{}
	counts, err := rebuild(context.Background(), identities, rooms, 100)
	if err != nil {
		t.Fatal(err)
	}
	if counts.identities != 2 || counts.rooms != 1 || counts.governance != 3 {
		t.Fatalf("counts=%+v", counts)
	}
	if identities.calls != 2 || rooms.roomCalls != 2 || rooms.governanceCalls != 2 {
		t.Fatalf("calls identity=%d room=%d governance=%d", identities.calls, rooms.roomCalls, rooms.governanceCalls)
	}
}
