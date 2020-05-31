package snpersist

import (
	"github.com/asdine/storm/v3"
	"github.com/jonhadfield/gosn-v2"
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
)

func TestSyncWithNoItems(t *testing.T) {
	sOutput, err := gosn.SignIn(sInput)
	assert.NoError(t, err, "sign-in failed", err)

	defer cleanup(&sOutput.Session)

	var so SyncOutput
	so, err = Sync(SyncInput{
		Session: sOutput.Session,
		DBPath:  tempDBPath,
	})
	assert.NoError(t, err)
	assert.NotEmpty(t, so.syncToken) // tells us what time to sync from next time
	assert.Empty(t, so.SavedItems)
}

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

	// get all items
	var allPersistedItems []Item
	err = db.All(&allPersistedItems)

	// items convert new items to 'persist' items
	itp := convertItemsToPersistItems(eItems)
	// add items
	allPersistedItems = append(allPersistedItems, itp...)

	err = db.Save(allPersistedItems)
	db.Close()

	var so SyncOutput
	so, err = Sync(SyncInput{
		Session: sOutput.Session,
		DBPath:  tempDBPath,
	})
	assert.NoError(t, err)
	assert.NotEmpty(t, so.syncToken) // tells us what time to sync from next time
	assert.Empty(t, so.cursorToken)  // empty because only default Items exist so no paging required
}

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
		Items: eItems,
	})
	assert.NoError(t, err)
	assert.Len(t, gso.SavedItems, 1)

	// open database
	var db *storm.DB
	db, err = storm.Open(tempDBPath)
	if err != nil {
		return
	}
	defer db.Close()
	defer removeDB(tempDBPath)

	// get all items
	var allPersistedItems []Item
	err = db.All(&allPersistedItems)
	assert.NoError(t, err)
	assert.Len(t, allPersistedItems, 0)
	db.Close()

	var so SyncOutput
	so, err = Sync(SyncInput{
		Session: sOutput.Session,
		DBPath:  tempDBPath,
	})
	assert.NoError(t, err)
	assert.NotEmpty(t, so.syncToken) // tells us what time to sync from next time
	assert.Empty(t, so.cursorToken)  // empty because only default Items exist so no paging required

	db, err = storm.Open(tempDBPath)
	assert.NoError(t, err)
	err = db.All(&allPersistedItems)
	var foundNotes int
	for _, pi := range allPersistedItems {
		if pi.ContentType == "Note" {
			foundNotes++
		}
	}
	assert.Equal(t, 1, foundNotes)
	//assert.Len(t, allPersistedItems, 1)
}

const tempDBPath = "test.db"

func removeDB(dbPath string) {
	os.Remove(dbPath)
}
