package snpersist

import (
	"github.com/asdine/storm/v3"
	"github.com/jonhadfield/gosn-v2"
	bolt "go.etcd.io/bbolt"
	"strings"
	"time"
)

//
//func main() {
//	db, err := storm.Open("my.db")
//	if err != nil {
//		panic(err)
//	}
//
//	defer db.Close()
//}

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
	DBPath  string
	//Items       gosn.EncryptedItems
	//SyncToken   string
	//CursorToken string
}

type SyncOutput struct {
	Items, SavedItems, Unsaved gosn.EncryptedItems // only used for testing purposes!?
	syncToken, cursorToken     string              // only used for testing purposes!?
}

func convertItemsToPersistItems(in gosn.EncryptedItems) (out []Item) {
	for _, i := range in {
		out = append(out, Item{
			UUID:        i.UUID,
			Content:     i.Content,
			ContentType: i.ContentType,
			EncItemKey:  i.EncItemKey,
			Deleted:     i.Deleted,
			CreatedAt:   i.CreatedAt,
			UpdatedAt:   i.UpdatedAt,
			Dirty:       true,
			DirtiedDate: time.Now(),
		})
	}
	return
}

func Sync(si SyncInput) (so SyncOutput, err error) {
	// open db
	var db *storm.DB
	db, err = storm.Open(si.DBPath, storm.BoltOptions(0600, &bolt.Options{Timeout: 2 * time.Second}))
	if err != nil {
		return
	}
	defer db.Close()

	// get dirty Items
	var dirty []Item
	err = db.Find("Dirty", true, &dirty)
	if err != nil {
		if !strings.Contains(err.Error(), "not found") {
			return
		}
	}

	// get sync token from previous operation
	var syncTokens []SyncToken
	err = db.All(&syncTokens)
	if err != nil {
		if !strings.Contains(err.Error(), "not found") {
			return
		}

		return
	}

	// convert dirty to gosn.Items
	var itemsToPush gosn.EncryptedItems
	for _, d := range dirty {
		itemsToPush = append(itemsToPush, gosn.EncryptedItem{
			UUID:        d.UUID,
			Content:     d.Content,
			ContentType: d.ContentType,
			EncItemKey:  d.EncItemKey,
			Deleted:     d.Deleted,
			CreatedAt:   d.CreatedAt,
			UpdatedAt:   d.UpdatedAt,
		})
	}

	// call gosn sync with new Items
	gSI := gosn.SyncInput{
		Session: si.Session,
		Items:   itemsToPush,
	}

	var gSO gosn.SyncOutput

	gSO, err = gosn.Sync(gSI)
	if err != nil {
		return
	}

	so.syncToken = gSO.SyncToken
	so.cursorToken = gSO.Cursor
	so.Items = gSO.Items
	so.SavedItems = gSO.SavedItems
	so.Unsaved = gSO.Unsaved

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
