package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitriy/gophkeeper/internal/client/gateway"
	"github.com/dmitriy/gophkeeper/internal/client/model"
)

// fakeEntryGateway is a hand-written gateway.EntryGateway returning
// canned results and recording the arguments of the last call.
type fakeEntryGateway struct {
	createOut *model.Entry
	createErr error
	getOut    *model.Entry
	getErr    error
	listOut   []*model.Entry
	listErr   error
	updateOut *model.Entry
	updateErr error
	deleteErr error
	syncOut   []*model.Entry
	syncErr   error

	lastCreate *model.Entry
	lastUpdate *model.Entry
	lastGetID  string
	lastDelID  string
}

func (f *fakeEntryGateway) Create(_ context.Context, entry *model.Entry) (*model.Entry, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.lastCreate = entry
	return f.createOut, nil
}

func (f *fakeEntryGateway) Get(_ context.Context, id string) (*model.Entry, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	f.lastGetID = id
	return f.getOut, nil
}

func (f *fakeEntryGateway) List(context.Context) ([]*model.Entry, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listOut, nil
}

func (f *fakeEntryGateway) Update(_ context.Context, entry *model.Entry) (*model.Entry, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	f.lastUpdate = entry
	return f.updateOut, nil
}

func (f *fakeEntryGateway) Delete(_ context.Context, id string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.lastDelID = id
	return nil
}

func (f *fakeEntryGateway) Sync(context.Context) ([]*model.Entry, error) {
	if f.syncErr != nil {
		return nil, f.syncErr
	}
	return f.syncOut, nil
}

// validEntry returns an entry passing all validation rules.
func validEntry() *model.Entry {
	return &model.Entry{
		Type:  model.EntryTypeLoginPassword,
		Label: "github",
		Data:  []byte("alice:secret"),
	}
}

func TestEntryServiceAddSuccess(t *testing.T) {
	out := &model.Entry{ID: "e1", Label: "github", Version: 1}
	gw := &fakeEntryGateway{createOut: out}
	svc := NewEntryService(gw)
	in := validEntry()

	got, err := svc.Add(context.Background(), in)

	require.NoError(t, err)
	assert.Same(t, out, got)
	assert.Same(t, in, gw.lastCreate, "the entry must be passed to the gateway as-is")
}

func TestEntryServiceAddValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(e *model.Entry)
		wantErr error
	}{
		{"nil entry", func(_ *model.Entry) {}, ErrEmptyData},
		{"unspecified type", func(e *model.Entry) { e.Type = model.EntryTypeUnspecified }, ErrInvalidEntryType},
		{"invalid type", func(e *model.Entry) { e.Type = model.EntryType(42) }, ErrInvalidEntryType},
		{"empty label", func(e *model.Entry) { e.Label = "" }, ErrEmptyLabel},
		{"label too long", func(e *model.Entry) { e.Label = strings.Repeat("x", 256) }, ErrLabelTooLong},
		{"metadata too long", func(e *model.Entry) { e.Metadata = strings.Repeat("x", 10001) }, ErrMetadataTooLong},
		{"empty data", func(e *model.Entry) { e.Data = nil }, ErrEmptyData},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw := &fakeEntryGateway{}
			svc := NewEntryService(gw)
			entry := validEntry()
			if tt.name == "nil entry" {
				entry = nil
			} else {
				tt.mutate(entry)
			}

			_, err := svc.Add(context.Background(), entry)

			assert.ErrorIs(t, err, tt.wantErr)
			assert.Nil(t, gw.lastCreate, "gateway must not be called on validation failure")
		})
	}
}

func TestEntryServiceAddBoundaries(t *testing.T) {
	tests := []struct {
		name    string
		label   string
		meta    string
		wantErr error
	}{
		{"label 255 ok", strings.Repeat("x", 255), "", nil},
		{"label 256 fails", strings.Repeat("x", 256), "", ErrLabelTooLong},
		{"metadata 10000 ok", "ok", strings.Repeat("x", 10000), nil},
		{"metadata 10001 fails", "ok", strings.Repeat("x", 10001), ErrMetadataTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw := &fakeEntryGateway{createOut: &model.Entry{ID: "e1"}}
			svc := NewEntryService(gw)

			_, err := svc.Add(context.Background(), &model.Entry{
				Type:     model.EntryTypeText,
				Label:    tt.label,
				Metadata: tt.meta,
				Data:     []byte("payload"),
			})

			if tt.wantErr == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tt.wantErr)
			}
		})
	}
}

func TestEntryServiceListPassThrough(t *testing.T) {
	entries := []*model.Entry{{ID: "e1"}, {ID: "e2"}}
	gw := &fakeEntryGateway{listOut: entries}
	svc := NewEntryService(gw)

	got, err := svc.List(context.Background())

	require.NoError(t, err)
	assert.Equal(t, entries, got)
}

func TestEntryServiceGetSuccess(t *testing.T) {
	out := &model.Entry{ID: "e1", Label: "github"}
	gw := &fakeEntryGateway{getOut: out}
	svc := NewEntryService(gw)

	got, err := svc.Get(context.Background(), "e1")

	require.NoError(t, err)
	assert.Same(t, out, got)
	assert.Equal(t, "e1", gw.lastGetID)
}

func TestEntryServiceGetEmptyID(t *testing.T) {
	svc := NewEntryService(&fakeEntryGateway{})

	_, err := svc.Get(context.Background(), "")

	assert.ErrorIs(t, err, ErrEmptyEntryID)
}

func TestEntryServiceEditSuccessPassesVersion(t *testing.T) {
	out := &model.Entry{ID: "e1", Version: 2}
	gw := &fakeEntryGateway{updateOut: out}
	svc := NewEntryService(gw)
	in := &model.Entry{
		ID:      "e1",
		Type:    model.EntryTypeText,
		Label:   "renamed",
		Data:    []byte("new payload"),
		Version: 1,
	}

	got, err := svc.Edit(context.Background(), in)

	require.NoError(t, err)
	assert.Same(t, out, got)
	assert.Same(t, in, gw.lastUpdate)
	assert.Equal(t, int64(1), gw.lastUpdate.Version, "version must reach the gateway unchanged")
}

func TestEntryServiceEditValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(e *model.Entry)
		wantErr error
	}{
		{"nil entry", func(_ *model.Entry) {}, ErrEmptyEntryID},
		{"empty id", func(_ *model.Entry) {}, ErrEmptyEntryID},
		{"zero version", func(e *model.Entry) { e.Version = 0 }, ErrInvalidVersion},
		{"negative version", func(e *model.Entry) { e.Version = -1 }, ErrInvalidVersion},
		{"invalid type", func(e *model.Entry) { e.Type = model.EntryType(42) }, ErrInvalidEntryType},
		{"empty label", func(e *model.Entry) { e.Label = "" }, ErrEmptyLabel},
		{"label too long", func(e *model.Entry) { e.Label = strings.Repeat("x", 256) }, ErrLabelTooLong},
		{"metadata too long", func(e *model.Entry) { e.Metadata = strings.Repeat("x", 10001) }, ErrMetadataTooLong},
		{"empty data", func(e *model.Entry) { e.Data = nil }, ErrEmptyData},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw := &fakeEntryGateway{}
			svc := NewEntryService(gw)
			entry := &model.Entry{
				ID:      "e1",
				Type:    model.EntryTypeText,
				Label:   "github",
				Data:    []byte("payload"),
				Version: 1,
			}
			if tt.name == "empty id" {
				entry.ID = ""
			}
			if tt.name == "nil entry" {
				entry = nil
			}
			tt.mutate(entry)

			_, err := svc.Edit(context.Background(), entry)

			assert.ErrorIs(t, err, tt.wantErr)
			assert.Nil(t, gw.lastUpdate, "gateway must not be called on validation failure")
		})
	}
}

func TestEntryServiceEditUnspecifiedTypeAllowed(t *testing.T) {
	// The server treats the entry type as immutable on update and
	// only validates it when non-zero; the client mirrors that.
	gw := &fakeEntryGateway{updateOut: &model.Entry{ID: "e1"}}
	svc := NewEntryService(gw)

	_, err := svc.Edit(context.Background(), &model.Entry{
		ID:      "e1",
		Label:   "github",
		Data:    []byte("payload"),
		Version: 1,
	})

	assert.NoError(t, err)
}

func TestEntryServiceRemovePassThrough(t *testing.T) {
	gw := &fakeEntryGateway{}
	svc := NewEntryService(gw)

	err := svc.Remove(context.Background(), "e1")

	require.NoError(t, err)
	assert.Equal(t, "e1", gw.lastDelID)
}

func TestEntryServiceRemoveEmptyID(t *testing.T) {
	svc := NewEntryService(&fakeEntryGateway{})

	err := svc.Remove(context.Background(), "")

	assert.ErrorIs(t, err, ErrEmptyEntryID)
}

func TestEntryServiceSyncPassThrough(t *testing.T) {
	entries := []*model.Entry{{ID: "e1", Version: 3}}
	gw := &fakeEntryGateway{syncOut: entries}
	svc := NewEntryService(gw)

	got, err := svc.Sync(context.Background())

	require.NoError(t, err)
	assert.Equal(t, entries, got)
}

func TestEntryServiceErrorPropagation(t *testing.T) {
	tests := []struct {
		name string
		run  func(svc *EntryService) error
		gw   *fakeEntryGateway
		want error
	}{
		{
			"create conflict",
			func(svc *EntryService) error {
				_, err := svc.Add(context.Background(), validEntry())
				return err
			},
			&fakeEntryGateway{createErr: gateway.ErrConflict},
			gateway.ErrConflict,
		},
		{
			"get not found",
			func(svc *EntryService) error {
				_, err := svc.Get(context.Background(), "e1")
				return err
			},
			&fakeEntryGateway{getErr: gateway.ErrNotFound},
			gateway.ErrNotFound,
		},
		{
			"list unauthenticated",
			func(svc *EntryService) error {
				_, err := svc.List(context.Background())
				return err
			},
			&fakeEntryGateway{listErr: gateway.ErrUnauthenticated},
			gateway.ErrUnauthenticated,
		},
		{
			"update conflict",
			func(svc *EntryService) error {
				_, err := svc.Edit(context.Background(), &model.Entry{
					ID: "e1", Type: model.EntryTypeText, Label: "l", Data: []byte("d"), Version: 1,
				})
				return err
			},
			&fakeEntryGateway{updateErr: gateway.ErrConflict},
			gateway.ErrConflict,
		},
		{
			"delete not found",
			func(svc *EntryService) error {
				return svc.Remove(context.Background(), "e1")
			},
			&fakeEntryGateway{deleteErr: gateway.ErrNotFound},
			gateway.ErrNotFound,
		},
		{
			"sync unauthenticated",
			func(svc *EntryService) error {
				_, err := svc.Sync(context.Background())
				return err
			},
			&fakeEntryGateway{syncErr: gateway.ErrUnauthenticated},
			gateway.ErrUnauthenticated,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run(NewEntryService(tt.gw))
			assert.ErrorIs(t, err, tt.want)
		})
	}
}

// TestEntryValidationErrorParity documents that the client limits
// mirror the server-side service rules; it guards against the two
// drifting apart silently.
func TestEntryValidationErrorParity(t *testing.T) {
	assert.Equal(t, maxLabelLen, 255)
	assert.Equal(t, maxMetadataLen, 10000)
	assert.Equal(t, ErrEmptyLabel.Error(), "label must not be empty")
	assert.Equal(t, ErrLabelTooLong.Error(), "label is too long")
	assert.Equal(t, ErrMetadataTooLong.Error(), "metadata is too long")
	assert.Equal(t, ErrEmptyData.Error(), "data must not be empty")
	assert.Equal(t, ErrInvalidEntryType.Error(), "invalid entry type")
	assert.Equal(t, ErrEmptyEntryID.Error(), "entry id must not be empty")
	assert.Equal(t, ErrInvalidVersion.Error(), "version must be at least 1")
}
