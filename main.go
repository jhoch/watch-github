package main

import (
	"context"
	"fmt"

	"github.com/die-net/lrucache"
	"github.com/google/go-github/github"
	"github.com/gregjones/httpcache"
	"github.com/spf13/viper"
	"golang.org/x/oauth2"
)

const ONE_HUNDRED_MEGABYTES = 100 * 1024 * 1024
const NO_EXPIRATION = 0

func main() {
	viper.SetConfigName("config")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig()
	if err != nil {
		panic(fmt.Errorf("Fatal error config file: %s \n", err))
	}
	ctx := context.Background()
	tokenSource := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: viper.GetString("token")},
	)
	authingClient := oauth2.NewClient(ctx, tokenSource)
	cachingTransport := &httpcache.Transport{
		Transport:           authingClient.Transport,
		Cache:               lrucache.New(ONE_HUNDRED_MEGABYTES, NO_EXPIRATION),
		MarkCachedResponses: true,
	}
	client := github.NewClient(cachingTransport.Client())
	events, response, err := client.Activity.ListRepositoryEvents(ctx, viper.GetString("org"), viper.GetString("repo"), &github.ListOptions{Page: 1})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Rate: %v %v %v\n", response.Rate.Limit, response.Rate.Remaining, response.Rate.Reset)
	fmt.Printf("%v\n", response.Header.Get("x-from-cache"))
	for i, event := range events {
		fmt.Printf("%v. %v (%v)\n", i+1, *event.Type, *event.Actor.Login)
	}
}
