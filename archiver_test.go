package archiver

import (
	"context"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func setup(t *testing.T) *sqlx.DB {
	testDB, err := ioutil.ReadFile("testdb.sql")
	assert.NoError(t, err)

	db, err := sqlx.Open("postgres", "postgres://localhost/archiver_test?sslmode=disable")
	assert.NoError(t, err)

	_, err = db.Exec(string(testDB))
	assert.NoError(t, err)
	logrus.SetLevel(logrus.DebugLevel)

	return db
}

func TestGetArchiveTasks(t *testing.T) {
	db := setup(t)

	// get the tasks for our org
	ctx := context.Background()
	orgs, err := GetActiveOrgs(ctx, db)
	assert.NoError(t, err)

	now := time.Date(2018, 1, 8, 12, 30, 0, 0, time.UTC)

	// org 1 is too new, no tasks
	tasks, err := GetMissingArchives(ctx, db, now, orgs[0], MessageType)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(tasks))

	// org 2 should have some
	tasks, err = GetMissingArchives(ctx, db, now, orgs[1], MessageType)
	assert.NoError(t, err)
	assert.Equal(t, 62, len(tasks))
	assert.Equal(t, time.Date(2017, 8, 10, 0, 0, 0, 0, time.UTC), tasks[0].StartDate)
	assert.Equal(t, time.Date(2017, 10, 10, 0, 0, 0, 0, time.UTC), tasks[61].StartDate)

	// org 3 is the same as 2, but two of the tasks have already been built
	tasks, err = GetMissingArchives(ctx, db, now, orgs[2], MessageType)
	assert.NoError(t, err)
	assert.Equal(t, 60, len(tasks))
	assert.Equal(t, time.Date(2017, 8, 11, 0, 0, 0, 0, time.UTC), tasks[0].StartDate)
	assert.Equal(t, time.Date(2017, 10, 10, 0, 0, 0, 0, time.UTC), tasks[59].StartDate)
}

func TestCreateMsgArchive(t *testing.T) {
	db := setup(t)
	ctx := context.Background()

	err := EnsureTempArchiveDirectory("/tmp")
	assert.NoError(t, err)

	orgs, err := GetActiveOrgs(ctx, db)
	assert.NoError(t, err)
	now := time.Date(2018, 1, 8, 12, 30, 0, 0, time.UTC)

	tasks, err := GetMissingArchives(ctx, db, now, orgs[1], MessageType)
	assert.NoError(t, err)
	assert.Equal(t, 62, len(tasks))
	task := tasks[0]

	// build our first task, should have no messages
	err = CreateArchiveFile(ctx, db, task, "/tmp")
	assert.NoError(t, err)

	// should have no records and be an empty gzip file
	assert.Equal(t, 0, task.RecordCount)
	assert.Equal(t, int64(23), task.ArchiveSize)
	assert.Equal(t, "f0d79988b7772c003d04a28bd7417a62", task.ArchiveHash)

	DeleteArchiveFile(task)

	// build our third task, should have a single message
	task = tasks[2]
	err = CreateArchiveFile(ctx, db, task, "/tmp")
	assert.NoError(t, err)

	// should have two records, second will have attachments
	assert.Equal(t, 2, task.RecordCount)
	assert.Equal(t, int64(442), task.ArchiveSize)
	assert.Equal(t, "7c39eb3244c34841cf5ca0382519142e", task.ArchiveHash)

	DeleteArchiveFile(task)
	_, err = os.Stat(task.ArchiveFile)
	assert.True(t, os.IsNotExist(err))
}

func TestWriteArchiveToDB(t *testing.T) {
	db := setup(t)
	ctx := context.Background()

	orgs, err := GetActiveOrgs(ctx, db)
	assert.NoError(t, err)
	now := time.Date(2018, 1, 8, 12, 30, 0, 0, time.UTC)

	tasks, err := GetMissingArchives(ctx, db, now, orgs[1], MessageType)
	assert.NoError(t, err)
	assert.Equal(t, 62, len(tasks))
	assert.Equal(t, time.Date(2017, 8, 10, 0, 0, 0, 0, time.UTC), tasks[0].StartDate)

	task := tasks[0]
	err = WriteArchiveToDB(ctx, db, task)

	assert.NoError(t, err)
	assert.Equal(t, 3, task.ID)
	assert.Equal(t, false, task.IsPurged)

	// if we recalculate our tasks, we should have one less now
	tasks, err = GetMissingArchives(ctx, db, now, orgs[1], MessageType)
	assert.NoError(t, err)
	assert.Equal(t, 61, len(tasks))
	assert.Equal(t, time.Date(2017, 8, 11, 0, 0, 0, 0, time.UTC), tasks[0].StartDate)
}

func TestArchiveOrg(t *testing.T) {
	db := setup(t)
	ctx := context.Background()

	orgs, err := GetActiveOrgs(ctx, db)
	assert.NoError(t, err)
	now := time.Date(2018, 1, 8, 12, 30, 0, 0, time.UTC)

	conf := NewConfig()
	conf.UploadToS3 = false
	conf.TempDir = "/tmp"

	archives, err := ArchiveOrg(ctx, now, conf, db, nil, orgs[1], MessageType)

	assert.Equal(t, 62, len(archives))
	assert.Equal(t, time.Date(2017, 8, 10, 0, 0, 0, 0, time.UTC), archives[0].StartDate)
	assert.Equal(t, time.Date(2017, 10, 10, 0, 0, 0, 0, time.UTC), archives[61].StartDate)

	assert.Equal(t, 0, archives[0].RecordCount)
	assert.Equal(t, int64(23), archives[0].ArchiveSize)
	assert.Equal(t, "f0d79988b7772c003d04a28bd7417a62", archives[0].ArchiveHash)

	assert.Equal(t, 2, archives[2].RecordCount)
	assert.Equal(t, int64(442), archives[2].ArchiveSize)
	assert.Equal(t, "7c39eb3244c34841cf5ca0382519142e", archives[2].ArchiveHash)
}
