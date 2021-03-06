package snpersist

import (
	"fmt"
	"github.com/asdine/storm/v3"
	"github.com/jonhadfield/gosn-v2"
	"strings"
	"time"
)

type Item struct {
	UUID        string `storm:"id,unique"`
	Content     string
	ContentType string `storm:"index"`
	EncItemKey  string
	Deleted     bool `storm:"index"`
	CreatedAt   string
	UpdatedAt   string
	Dirty       bool
	DirtiedDate time.Time
}

type SyncToken struct {
	SyncToken string `storm:"id,unique"`
}

// persist.sync is a wrapper around gosn.sync and local database updates

// persist.sync is triggered:
// 1) every time a new item is added
// 2) every time a query is performed

// persist.sync will
// 1) create a slice of dirty Items
// 2) call gosn.sync with the dirty Items
// 3) IF SUCCESSFUL...
// 		- mark dirty Items as clean
//		- persist new Items to db

// as a cli user I edit a note (specify uuid?) that's in the db then save the changes
// 1) on load, cli should check if db exists, if not, populate (call sync)
// 2) user edits note pulled from db
// 3) after changes, save to db (mark as dirty) and call sync
// 4) sync will put the dirty item in the gosn.sync input, run gosn.sync to push the item and pull the new Items ALSO save new sync and cursor tokens!
// 5) db will now be up-to-date with nothing dirty, unless there was a persist.sync error returned

type SyncInput struct {
	Session gosn.Session
	DB      *storm.DB // pointer to an existing DB
	DBPath  string    // path to create new DB
}

type SyncOutput struct {
	Items, SavedItems, Unsaved gosn.EncryptedItems // only used for testing purposes!?
	//syncToken, cursorToken     string              // only used for testing purposes!?
	DB *storm.DB // pointer to DB (same if passed in SyncInput, new if called without existing)
}

type Items []Item

func (pi Items) ToItems(session gosn.Session) (items gosn.Items, err error) {
	var eItems gosn.EncryptedItems
	for _, ei := range pi {
		eItems = append(eItems, gosn.EncryptedItem{
			UUID:        ei.UUID,
			Content:     ei.Content,
			ContentType: ei.ContentType,
			EncItemKey:  ei.EncItemKey,
			Deleted:     ei.Deleted,
			CreatedAt:   ei.CreatedAt,
			UpdatedAt:   ei.UpdatedAt,
		})
	}
	if eItems != nil {
		items, err = eItems.DecryptAndParse(session.Mk, session.Ak, false)
	}

	return
}

func ConvertItemsToPersistItems(items gosn.EncryptedItems) (pitems []Item) {
	for _, i := range items {
		pitems = append(pitems, Item{
			UUID:        i.UUID,
			Content:     i.Content,
			ContentType: i.ContentType,
			EncItemKey:  i.EncItemKey,
			Deleted:     i.Deleted,
			CreatedAt:   i.CreatedAt,
			UpdatedAt:   i.UpdatedAt,
		})
	}

	return
}

func initialiseDB(si SyncInput) (db *storm.DB, err error) {
	// create new DB in provided path
	db, err = storm.Open(si.DBPath)
	if err != nil {
		return
	}

	// call gosn sync to get existing items
	gSI := gosn.SyncInput{
		Session: si.Session,
	}

	var gSO gosn.SyncOutput

	gSO, err = gosn.Sync(gSI)
	if err != nil {
		return
	}

	// put new Items in db
	for _, i := range gSO.Items {
		item := Item{
			UUID:        i.UUID,
			Content:     i.Content,
			ContentType: i.ContentType,
			EncItemKey:  i.EncItemKey,
			Deleted:     i.Deleted,
			CreatedAt:   i.CreatedAt,
			UpdatedAt:   i.UpdatedAt,
		}
		err = db.Save(&item)
		if err != nil {
			return
		}
	}

	// update sync values in db for next time
	sv := SyncToken{
		SyncToken: gSO.SyncToken,
	}
	if err = db.Save(&sv); err != nil {
		return
	}

	return
}

func Sync(si SyncInput) (so SyncOutput, err error) {
	if !si.Session.Valid() {
		err = fmt.Errorf("invalid session")
		return
	}

	if si.DB != nil && si.DBPath != "" {
		err = fmt.Errorf("passing a DB pointer and DB path does not make sense")
		return
	}

	if si.DB == nil {
		if si.DBPath == "" {
			err = fmt.Errorf("DB pointer or DB path are required")
			return
		}
		var db *storm.DB
		db, err = initialiseDB(si)
		return SyncOutput{
			DB: db,
		}, err
	}

	// get dirty Items
	var dirty []Item
	err = si.DB.Find("Dirty", true, &dirty)
	if err != nil {
		if !strings.Contains(err.Error(), "not found") {
			return
		}
	}

	// get sync token from previous operation
	var syncTokens []SyncToken
	err = si.DB.All(&syncTokens)
	if err != nil {
		if !strings.Contains(err.Error(), "not found") {
			return
		}

		return
	}
	var syncToken string
	if len(syncTokens) > 1 {
		err = fmt.Errorf("expected maximum of one sync token but %d returned", len(syncTokens))
	}
	if len(syncTokens) == 1 {
		syncToken = syncTokens[0].SyncToken
	}

	// convert dirty to gosn.Items
	var dirtyItemsToPush gosn.EncryptedItems
	for _, d := range dirty {
		dirtyItemsToPush = append(dirtyItemsToPush, gosn.EncryptedItem{
			UUID:        d.UUID,
			Content:     d.Content,
			ContentType: d.ContentType,
			EncItemKey:  d.EncItemKey,
			Deleted:     d.Deleted,
			CreatedAt:   d.CreatedAt,
			UpdatedAt:   d.UpdatedAt,
		})
	}

	// call gosn sync with dirty items to push
	gSI := gosn.SyncInput{
		Session:   si.Session,
		Items:     dirtyItemsToPush,
		SyncToken: syncToken,
	}

	var gSO gosn.SyncOutput

	gSO, err = gosn.Sync(gSI)
	if err != nil {
		return
	}

	// TODO: Remove dirty flag from DB after successful push
	for _, d := range dirty {
		err = si.DB.UpdateField(&Item{UUID: d.UUID}, "Dirty", false)
		if err != nil {
			return
		}
		err = si.DB.UpdateField(&Item{UUID: d.UUID}, "DirtiedDate", time.Time{})
		if err != nil {
			return
		}
	}

	so.Items = gSO.Items
	so.SavedItems = gSO.SavedItems
	so.Unsaved = gSO.Unsaved
	so.DB = si.DB

	// put new Items in db
	for _, i := range gSO.Items {
		item := Item{
			UUID:        i.UUID,
			Content:     i.Content,
			ContentType: i.ContentType,
			EncItemKey:  i.EncItemKey,
			Deleted:     i.Deleted,
			CreatedAt:   i.CreatedAt,
			UpdatedAt:   i.UpdatedAt,
		}
		err = si.DB.Save(&item)
		if err != nil {
			return
		}
	}

	// update sync values in db for next time
	sv := SyncToken{
		SyncToken: gSO.SyncToken,
	}
	if err = si.DB.Save(&sv); err != nil {
		return
	}

	return
}
