package syncFStoGithub

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"cloud.google.com/go/functions/metadata"
	_ "github.com/GoogleCloudPlatform/functions-framework-go/funcframework"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	gogitConfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
)

// FirestoreEvent is the payload of a Firestore event.
type FirestoreEvent struct {
	OldValue FirestoreValue `json:"oldValue"`
	Value    FirestoreValue `json:"value"`
}

// FirestoreValue holds Firestore fields
type FirestoreValue struct {
	CreateTime time.Time `json:"createTime"`
	Fields     FVRecord  `json:"fields"`
	Name       string    `json:"name"`
	UpdateTime time.Time `json:"updateTime"`
}

// Record list the fields that need to be listened
type FVRecord struct {
	ID struct {
		StringValue string `json:"stringValue"`
	} `json:"ID"`
	FirstName struct {
		StringValue string `json:"stringValue"`
	} `json:"FirstName"`
	LastName struct {
		StringValue string `json:"stringValue"`
	} `json:"LastName"`
	Birthday struct {
		StringValue string `json:"stringValue"`
	} `json:"Birthday"`
}

type Record struct {
	ID        string `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Birthday  string `json:"birthday"`
}

var (
	fsClient     *firestore.Client
	projectID    string
	githubURL    string
	githubBranch string
	githubToken  string
	githubEmail  string
)

// SyncFirestoreToGithub is triggered by a change to a Firestore document.
func SyncFirestoreToGithub(ctx context.Context, event FirestoreEvent) error {
	var err error

	githubURL = os.Getenv("GITHUB_URL")
	githubBranch = os.Getenv("GITHUB_BRANCH")
	githubToken = os.Getenv("GITHUB_TOKEN")
	githubEmail = os.Getenv("GITHUB_EMAIL")

	projectID = os.Getenv("GOOGLE_PROJECT_ID")
	fsClient, err = firestore.NewClient(ctx, projectID)
	if err != nil {
		return fmt.Errorf("cannot create Firestore client: %v", err)
	}
	defer fsClient.Close()

	meta, err := metadata.FromContext(ctx)
	if err != nil {
		return fmt.Errorf("metadata.FromContext: %v", err)
	}

	paths := strings.Split(meta.Resource.RawPath, "/")
	recordID := paths[len(paths)-1]

	//check if the event is triggered because of Delete
	if event.Value.Fields.ID.StringValue == "" {
		err = deleteFromGithub(ctx, recordID)
		if err != nil {
			return fmt.Errorf("deleteFromGithub (recordID: %v) err: %v", recordID, err)
		}
	} else {
		recordID := event.Value.Fields.ID.StringValue
		record := Record{
			ID:        recordID,
			FirstName: event.Value.Fields.FirstName.StringValue,
			LastName:  event.Value.Fields.LastName.StringValue,
			Birthday:  event.Value.Fields.Birthday.StringValue,
		}

		err = updateGithub(ctx, recordID, record)
		if err != nil {
			return fmt.Errorf("updateGithub (recordID: %v) err: %v", recordID, err)
		}
	}

	return nil
}

func updateGithub(ctx context.Context, recordID string, recordDoc Record) error {
	memoryStorage := memory.NewStorage()
	fs := memfs.New()

	githubAuth := &http.BasicAuth{
		Username: githubEmail,
		Password: githubToken,
	}

	// Clone the given repository
	repo, err := git.Clone(memoryStorage, fs, &git.CloneOptions{
		Auth: githubAuth,
		URL:  githubURL,
	})
	if err != nil {
		return err
	}

	w, err := repo.Worktree()
	if err != nil {
		return err
	}

	err = repo.Fetch(&git.FetchOptions{
		Auth:     githubAuth,
		RefSpecs: []gogitConfig.RefSpec{"refs/*:refs/*", "HEAD:refs/heads/HEAD"},
	})
	if err != nil {
		return err
	}

	// checkout appropriate branch
	err = w.Checkout(&git.CheckoutOptions{
		Branch: plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", githubBranch)),
		Force:  true,
	})
	if err != nil {
		return err
	}

	// create / update file inside of the worktree of the project
	filename := recordID + ".json"
	file, err := fs.OpenFile(filename, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return err
	}

	recordDocJSON, err := json.MarshalIndent(recordDoc, "", "\t")
	if err != nil {
		return err
	}
	file.Write(recordDocJSON)
	file.Close()

	// Adds the new file to the staging area
	_, err = w.Add(filename)
	if err != nil {
		return err
	}

	// Get the status of the worktree
	status, err := w.Status()
	if err != nil {
		return err
	}

	// Only commit and push to remote if there is modification
	if !status.IsClean() {
		// Commits the current staging area to the repository
		_, err = w.Commit("Create / Update recordID: "+recordID, &git.CommitOptions{
			Author: &object.Signature{
				Name:  githubEmail,
				Email: githubEmail,
				When:  time.Now(),
			},
		})
		if err != nil {
			return err
		}

		//Push the code to the remote
		err = repo.Push(&git.PushOptions{
			Auth:       githubAuth,
			RemoteName: "origin",
			RefSpecs:   []gogitConfig.RefSpec{gogitConfig.RefSpec(fmt.Sprintf("+refs/heads/%s:refs/heads/%s", githubBranch, githubBranch))},
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func deleteFromGithub(ctx context.Context, recordID string) error {
	memoryStorage := memory.NewStorage()
	fs := memfs.New()

	githubAuth := &http.BasicAuth{
		Username: githubEmail,
		Password: githubToken,
	}

	// Clone the given repository
	repo, err := git.Clone(memoryStorage, fs, &git.CloneOptions{
		Auth: githubAuth,
		URL:  githubURL,
	})
	if err != nil {
		return err
	}

	w, err := repo.Worktree()
	if err != nil {
		return err
	}

	err = repo.Fetch(&git.FetchOptions{
		Auth:     githubAuth,
		RefSpecs: []gogitConfig.RefSpec{"refs/*:refs/*", "HEAD:refs/heads/HEAD"},
	})
	if err != nil {
		return err
	}

	// checkout appropriate branch
	err = w.Checkout(&git.CheckoutOptions{
		Branch: plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", githubBranch)),
		Force:  true,
	})
	if err != nil {
		return err
	}

	// remove file inside of the worktree of the project
	filename := recordID + ".json"
	_, err = w.Remove(filename)
	if err != nil {
		return err
	}

	// Commits the current staging area to the repository
	_, err = w.Commit("Remove recordID: "+recordID, &git.CommitOptions{
		Author: &object.Signature{
			Name:  githubEmail,
			Email: githubEmail,
			When:  time.Now(),
		},
	})
	if err != nil {
		return err
	}

	//Push the code to the remote
	err = repo.Push(&git.PushOptions{
		Auth:       githubAuth,
		RemoteName: "origin",
		RefSpecs:   []gogitConfig.RefSpec{gogitConfig.RefSpec(fmt.Sprintf("+refs/heads/%s:refs/heads/%s", githubBranch, githubBranch))},
	})
	if err != nil {
		return err
	}

	return nil
}
