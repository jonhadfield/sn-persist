package snpersist

import (
	"fmt"
	"github.com/asdine/storm/v3"
	"github.com/davecgh/go-spew/spew"
	"github.com/jonhadfield/gosn-v2"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestSyncWithoutDatabase(t *testing.T) {
	sOutput, err := gosn.SignIn(sInput)
	assert.NoError(t, err, "sign-in failed", err)
	_, err = Sync(SyncInput{Session: sOutput.Session})
	assert.EqualError(t, err, "DB pointer or DB path are required")
}

func TestSyncWithInvalidSession(t *testing.T) {
	defer removeDB(tempDBPath)
	db, err := storm.Open(tempDBPath)
	assert.NoError(t, err)
	// missing session
	_, err = Sync(SyncInput{DB: db})
	assert.EqualError(t, err, "invalid session")
	_, err = Sync(SyncInput{DB: db, Session: gosn.Session{
		Token: "a",
		Mk:    "b",
		Ak:    "c",
	}})
	assert.EqualError(t, err, "invalid session")
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
	fmt.Println(syncTokens[0])
	assert.Empty(t, so.SavedItems)
	so.DB.Close()
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

	//var so SyncOutput
	_, err = Sync(SyncInput{
		Session: sOutput.Session,
		DB:      db,
	})
	assert.NoError(t, err)
	//assert.NotEmpty(t, so.syncToken) // tells us what time to sync from next time
	//assert.Empty(t, so.cursorToken)  // empty because only default Items exist so no paging required
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
		Items:   eItems,
	})
	assert.NoError(t, err)
	assert.Len(t, gso.SavedItems, 1)

	// initialise DB
	var so SyncOutput
	so, err = Sync(SyncInput{
		Session: sOutput.Session,
		DBPath:  tempDBPath,
	})
	spew.Dump(so)
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
			foundNotes++
		}
	}
	assert.Equal(t, 1, foundNotes)
}
