package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strconv"
	"time"

	"github.com/die-net/lrucache"
	"github.com/google/go-github/github"
	"github.com/gregjones/httpcache"
	"github.com/spf13/viper"
	"golang.org/x/oauth2"
)

const DEFAULT_POLL_INTERVAL = 60
const ONE_HUNDRED_MEGABYTES = 100 * 1024 * 1024
const NO_EXPIRATION = 0
const SENTINEL_ID = ""

type eventClient struct {
	Client *github.Client
	Owner  string
	Repo   string
}

type origination struct {
	ID        string
	CreatedAt *time.Time
}

var SENTINEL_ORIGINATION = origination{ID: SENTINEL_ID, CreatedAt: nil}

func main() {
	readInConfig()
	ctx := context.Background()
	client := createEventClient(
		ctx,
		viper.GetString("token"),
		viper.GetString("owner"),
		viper.GetString("repo"))
	dataDir := viper.GetString("data-dir")
	var recentEvent = readMostRecentEvent(dataDir)
	for {
		newEvents, pollInterval, err := client.fetchEventsAfter(ctx, newOrigination(recentEvent))
		if err != nil {
			fmt.Printf("error: %v\n", err)
			return
		}
		if len(newEvents) > 0 {
			err := persistEvents(dataDir, newEvents)
			if err != nil {
				fmt.Printf("error: %v\n", err)
			}
			recentEvent = newEvents[0]
		}
		time.Sleep(time.Duration(pollInterval) * time.Second)
	}
}
func readInConfig() {
	viper.SetConfigName("config")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig()
	if err != nil {
		panic(fmt.Errorf("Fatal error config file: %s \n", err))
	}
	requireConfigKey("token")
	requireConfigKey("data-dir")
	requireConfigKey("owner")
	requireConfigKey("repo")
}

func requireConfigKey(key string) {
	if len(viper.GetString(key)) == 0 {
		panic(fmt.Errorf("No %s was specified.  Check that your config file is present and specifies '%s'.\n", key, key))
	}
}

func createEventClient(
	ctx context.Context,
	token string,
	owner string,
	repo string) *eventClient {
	tokenSource := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	authingClient := oauth2.NewClient(ctx, tokenSource)
	cachingTransport := &httpcache.Transport{
		Transport:           authingClient.Transport,
		Cache:               lrucache.New(ONE_HUNDRED_MEGABYTES, NO_EXPIRATION),
		MarkCachedResponses: true,
	}
	client := github.NewClient(cachingTransport.Client())
	return &eventClient{client, owner, repo}
}

func readMostRecentEvent(dataDir string) *github.Event {
	files, err := ioutil.ReadDir(dataDir)
	if err != nil {
		fmt.Printf("error: %v\n", err)
	}
	if len(files) > 0 {
		recent, err := os.Open(path.Join(dataDir, files[len(files)-1].Name()))
		if err != nil {
			fmt.Printf("error: %v\n", err)
		}
		defer recent.Close()
		var recentEvents []*github.Event
		err = json.NewDecoder(recent).Decode(&recentEvents)
		if err != nil {
			fmt.Printf("error: %v\n", err)
		}
		return recentEvents[0]
	}
	return nil
}

func newOrigination(event *github.Event) origination {
	if event == nil {
		return SENTINEL_ORIGINATION
	}
	return origination{*event.ID, event.CreatedAt}
}

func (client *eventClient) fetchEventsAfter(
	ctx context.Context,
	bookmark origination,
) ([]*github.Event, int, error) {
	var newEvents []*github.Event
	var response *github.Response
	var err error
	page := 1
pagination:
	for ; page > 0; page = response.NextPage {
		var events []*github.Event
		events, response, err = client.Client.Activity.ListRepositoryEvents(
			ctx, client.Owner, client.Repo, &github.ListOptions{Page: page})
		if err == nil {
			for _, event := range events {
				if *event.ID == bookmark.ID {
					break pagination
				}
				newEvents = append(newEvents, event)
			}
		}
		if response == nil {
			break
		}
	}
	if page == 0 {
		lastEvent := newEvents[len(newEvents)-1]
		fmt.Printf("events were dropped between %s (%v) and %s (%v)\n",
			*lastEvent.ID,
			lastEvent.CreatedAt,
			bookmark.ID,
			bookmark.CreatedAt,
		)
	}
	var pollInterval int
	if response == nil {
		pollInterval = DEFAULT_POLL_INTERVAL
	} else {
		var err error
		pollInterval, err = strconv.Atoi(response.Header.Get("x-poll-interval"))
		if err != nil {
			fmt.Printf("error parsing pollInterval (defaulting to 60): %v\n", err)
			pollInterval = DEFAULT_POLL_INTERVAL
		}
	}
	if pollInterval != DEFAULT_POLL_INTERVAL {
		fmt.Printf("abnormal pollInterval: %d\n", pollInterval)
	}
	return newEvents, pollInterval, err
}

func persistEvents(dataDir string, events []*github.Event) error {
	filename := fmt.Sprintf("%v-events.json", time.Now().UTC().Format(time.RFC3339))
	file, err := os.Create(path.Join(dataDir, filename))
	if err != nil {
		return err
	}
	defer file.Close()
	return json.NewEncoder(file).Encode(events)
}
