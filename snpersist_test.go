package snpersist

import (
	"github.com/asdine/storm/v3"
	"github.com/jonhadfield/gosn-v2"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestSyncWithoutDatabase(t *testing.T) {
	sOutput, err := gosn.SignIn(sInput)
	assert.NoError(t, err, "sign-in failed", err)
	_, err = Sync(SyncInput{Session: sOutput.Session})
	assert.EqualError(t, err, "DB pointer or DB path are required")
}

func TestSyncWithInvalidSession(t *testing.T) {
	defer removeDB(tempDBPath)
	_, err := Sync(SyncInput{DBPath: tempDBPath})
	assert.EqualError(t, err, "invalid session")
	_, err = Sync(SyncInput{DBPath: tempDBPath, Session: gosn.Session{
		Token: "a",
		Mk:    "b",
		Ak:    "c",
	}})
	assert.EqualError(t, err, "invalid session")
}

func TestSyncWithPathAndDB(t *testing.T) {
	sOutput, err := gosn.SignIn(sInput)
	assert.NoError(t, err, "sign-in failed", err)
	var db *storm.DB
	db, err = storm.Open(tempDBPath)
	assert.NoError(t, err)
	defer db.Close()
	defer removeDB(tempDBPath)
	_, err = Sync(SyncInput{DBPath: tempDBPath, DB: db, Session: sOutput.Session})
	assert.EqualError(t, err, "passing a DB pointer and DB path does not make sense")
}

func TestSyncWithNoItems(t *testing.T) {
	sOutput, err := gosn.SignIn(sInput)
	assert.NoError(t, err, "sign-in failed", err)

	defer cleanup(&sOutput.Session)

	defer removeDB(tempDBPath)

	var so SyncOutput
	so, err = Sync(SyncInput{
		Session: sOutput.Session,
		DBPath:  tempDBPath,
	})
	assert.NoError(t, err)

	var syncTokens []SyncToken
	err = so.DB.All(&syncTokens)
	assert.NoError(t, err)
	assert.Len(t, syncTokens, 1)
	assert.NotEmpty(t, syncTokens[0]) // tells us what time to sync from next time
	assert.Empty(t, so.SavedItems)
	so.DB.Close()
}

// create a note in a storm DB, mark it dirty, and then sync to SN
// the returned DB should have the note returned as not dirty
// SN should now have that note
func TestSyncWithNewNote(t *testing.T) {
	sOutput, err := gosn.SignIn(sInput)
	assert.NoError(t, err, "sign-in failed", err)

	defer cleanup(&sOutput.Session)

	// create new note with random content
	newNote, _ := createNote("test", "")
	dItems := gosn.Items{&newNote}
	assert.NoError(t, dItems.Validate())
	var eItems gosn.EncryptedItems
	eItems, err = dItems.Encrypt(sOutput.Session.Mk, sOutput.Session.Ak, true)
	assert.NoError(t, err)

	// open database
	var db *storm.DB
	db, err = storm.Open(tempDBPath)
	if err != nil {
		return
	}
	defer db.Close()
	defer removeDB(tempDBPath)

	var allPersistedItems []Item
	// items convert new items to 'persist' items and mark as dirty
	itp := ConvertItemsToPersistItems(eItems)
	for _, i := range itp {
		i.Dirty = true
		i.DirtiedDate = time.Now()
		assert.NoError(t, db.Save(&i))
		allPersistedItems = append(allPersistedItems, i)
	}

	assert.Len(t, allPersistedItems, 1)

	var so SyncOutput
	so, err = Sync(SyncInput{
		Session: sOutput.Session,
		DB:      db,
	})
	assert.NoError(t, err)

	assert.Len(t, so.SavedItems, 1)
	assert.Equal(t, newNote.UUID, so.SavedItems[0].UUID)
	assert.Equal(t, "Note", so.SavedItems[0].ContentType)

	assert.NoError(t, so.DB.All(&allPersistedItems))
	var foundNonDirtyNote bool
	for _, i := range allPersistedItems {
		if i.UUID == newNote.UUID {
			foundNonDirtyNote = true
			assert.False(t, i.Dirty)
			assert.Zero(t, i.DirtiedDate)
		}
	}
	assert.True(t, foundNonDirtyNote)

	// check the item exists in SN

	// get sync token from previous operation
	var syncTokens []SyncToken
	assert.NoError(t, so.DB.All(&syncTokens))
	assert.Len(t, syncTokens, 1)

	var gSO gosn.SyncOutput
	gSO, err = gosn.Sync(gosn.SyncInput{
		Session:   sOutput.Session,
		SyncToken: syncTokens[0].SyncToken,
	})
	assert.NoError(t, err)

	var foundNewItem bool
	for _, i := range gSO.Items {
		if i.UUID == newNote.UUID {
			foundNewItem = true
			assert.Equal(t, "Note", i.ContentType)
		}
	}
	assert.True(t, foundNewItem)
}

// create a note in SN directly
// call persist Sync and check DB contains the note
func TestSyncOneExisting(t *testing.T) {
	sOutput, err := gosn.SignIn(sInput)
	assert.NoError(t, err, "sign-in failed", err)

	defer cleanup(&sOutput.Session)

	// create new note with random content and push to SN (not DB)
	newNote, _ := createNote("test", "")
	dItems := gosn.Items{&newNote}
	assert.NoError(t, dItems.Validate())
	var eItems gosn.EncryptedItems
	eItems, err = dItems.Encrypt(sOutput.Session.Mk, sOutput.Session.Ak, true)
	assert.NoError(t, err)

	// push to SN
	var gso gosn.SyncOutput
	gso, err = gosn.Sync(gosn.SyncInput{
		Session: sOutput.Session,
		Items:   eItems,
	})
	assert.NoError(t, err)
	assert.Len(t, gso.SavedItems, 1)

	// call persist sync
	var so SyncOutput
	so, err = Sync(SyncInput{
		Session: sOutput.Session,
		DBPath:  tempDBPath,
	})
	assert.NoError(t, err)

	defer so.DB.Close()
	defer removeDB(tempDBPath)

	// get all items
	var allPersistedItems []Item
	err = so.DB.All(&allPersistedItems)
	assert.NoError(t, err)

	var syncTokens []SyncToken
	err = so.DB.All(&syncTokens)
	assert.NoError(t, err)
	assert.NotEmpty(t, syncTokens)

	err = so.DB.All(&allPersistedItems)
	var foundNotes int
	for _, pi := range allPersistedItems {
		if pi.ContentType == "Note" {
			assert.Equal(t, newNote.UUID, pi.UUID)
			foundNotes++
		}
	}
	assert.Equal(t, 1, foundNotes)
}
