package db

import (
	"encoding/binary"
	"fmt"
	"log"
	"time"

	"github.com/boltdb/bolt"
)

type dbItem struct {
	key       []byte
	datetime  time.Time
	thumbnail []byte
}

var dbWriteChannel = make(chan dbItem)

func Init() {
	go writeChannelMonitor()
}

func writeChannelMonitor() {
	for {
		dbItem := <-dbWriteChannel
		putImage(dbItem)
		fmt.Printf("Put in database: %s\n", dbItem.key)
	}
}

func PutImage(key []byte, datetime time.Time, img []byte) {
	dbWriteChannel <- dbItem{key, datetime, img}
}

func putImage(item dbItem) {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte("images"))
		if err != nil {
			return err
		}
		return b.Put(item.key, item.thumbnail)
	})

	db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte("datetime"))
		if err != nil {
			return err
		}

		// xyzzy something like this?
		//singleDateBucket, err := b.CreateBucketIfNotExists([]byte(item.datetime.String()))
		//singleDateBucket.Put(item.key, item.thumbnail)
		// instead of this...
		return b.Put([]byte(item.datetime.String()), item.key)
	})
}

func GetImage(key []byte) []byte {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	var buf []byte
	err = db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("images"))
		buf = b.Get(key)
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	// buf is lost on return (probably because of db.Close())
	bufCopy := make([]byte, len(buf), (cap(buf)+1)*2)
	copy(bufCopy, buf)
	return bufCopy
}

func KeyExists(key []byte) bool {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	var buf []byte
	err = db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("images"))
		if b != nil {
			buf = b.Get(key)
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	return buf != nil
}

func GetImageByIndex(i uint64) []byte {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	var buf []byte
	err = db.View(func(tx *bolt.Tx) error {
		index := tx.Bucket([]byte("index"))
		imageKey := index.Get(itob(i))

		images := tx.Bucket([]byte("images"))
		buf = images.Get(imageKey)
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	// buf is lost on return (probably because of db.Close())
	bufCopy := make([]byte, len(buf), (cap(buf)+1)*2)
	copy(bufCopy, buf)
	return bufCopy
}

func GetKeyByIndex(i uint64) []byte {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	var imageKey []byte
	err = db.View(func(tx *bolt.Tx) error {
		index := tx.Bucket([]byte("index"))
		imageKey = index.Get(itob(i))
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	// imageKey is lost on return (probably because of db.Close())
	imageKeyCopy := make([]byte, len(imageKey), (cap(imageKey)+1)*2)
	copy(imageKeyCopy, imageKey)
	return imageKeyCopy
}

func CreateIndex() {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	err = db.Update(func(tx *bolt.Tx) error {
		datetime, err := tx.CreateBucketIfNotExists([]byte("datetime"))
		if err != nil {
			return err
		}

		err = tx.DeleteBucket([]byte("index"))
		if err != bolt.ErrBucketNotFound {
			return err
		}
		index, err := tx.CreateBucket([]byte("index"))
		if err != nil {
			return err
		}

		datetimeCount := uint64(getLengthOfBucket(db, "datetime"))

		// Iterate over items in sorted key order.
		// Put in index in reverse datetime order.
		if err := datetime.ForEach(func(k, v []byte) error {
			id, _ := index.NextSequence()
			index.Put(itob(datetimeCount-id), v)
			return nil
		}); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
}

func GetLengthOfIndex() int {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	return getLengthOfBucket(db, "index")
}

func getLengthOfBucket(db *bolt.DB, bucketName string) int {
	var lengthOfIndex int
	err := db.View(func(tx *bolt.Tx) error {
		index := tx.Bucket([]byte(bucketName))
		lengthOfIndex = index.Stats().KeyN
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	return lengthOfIndex
}

func CreateIndex_xyzzyOrigDelete() {
	db, err := bolt.Open("chronoshot.db", 0777, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	err = db.Update(func(tx *bolt.Tx) error {
		images, err := tx.CreateBucketIfNotExists([]byte("images"))
		if err != nil {
			return err
		}

		err = tx.DeleteBucket([]byte("index"))
		if err != bolt.ErrBucketNotFound {
			return err
		}
		index, err := tx.CreateBucket([]byte("index"))
		if err != nil {
			return err
		}

		// Iterate over items in sorted key order.
		if err := images.ForEach(func(k, v []byte) error {
			id, _ := index.NextSequence()
			index.Put(itob(id), k)
			return nil
		}); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
}

// itob returns an 8-byte big endian representation of v.
func itob(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v))
	return b
}
