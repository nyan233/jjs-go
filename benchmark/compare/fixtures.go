//go:generate go run github.com/mailru/easyjson/easyjson@v0.9.2 -all fixtures.go

package compare

import (
	_ "embed"
	stdjson "encoding/json"
)

var (
	//go:embed testdata/small.json
	SmallJSON []byte

	//go:embed testdata/medium.json
	MediumJSON []byte

	//go:embed testdata/large.json
	LargeJSON []byte
)

// Book, Author and SmallJSON are adapted from Sonic's small benchmark fixture.
type Book struct {
	BookID  int       `json:"id"`
	BookIDs []int     `json:"ids"`
	Title   string    `json:"title"`
	Titles  []string  `json:"titles"`
	Price   float64   `json:"price"`
	Prices  []float64 `json:"prices"`
	Hot     bool      `json:"hot"`
	Hots    []bool    `json:"hots"`
	Author  Author    `json:"author"`
	Authors []Author  `json:"authors"`
	Weights []int     `json:"weights"`
}

type Author struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
	Male bool   `json:"male"`
}

var SmallBook = Book{
	BookID:  12125925,
	BookIDs: []int{-2147483648, 2147483647},
	Title:   "未来简史-从智人到智神",
	Titles:  []string{"hello", "world"},
	Price:   40.8,
	Prices:  []float64{-0.1, 0.1},
	Hot:     true,
	Hots:    []bool{true, true, true},
	Author:  Author{Name: "json", Age: 99, Male: true},
	Authors: []Author{
		{Name: "json", Age: 99, Male: true},
		{Name: "json", Age: 99, Male: true},
		{Name: "json", Age: 99, Male: true},
	},
	Weights: []int{},
}

// The Twitter fixture types below match Sonic's benchmark schema.
type TwitterResponse struct {
	Statuses       []Status       `json:"statuses"`
	SearchMetadata SearchMetadata `json:"search_metadata"`
}

type Hashtag struct {
	Text    string `json:"text"`
	Indices []int  `json:"indices"`
}

type Entities struct {
	URLs         []any     `json:"urls"`
	Hashtags     []Hashtag `json:"hashtags"`
	UserMentions []any     `json:"user_mentions"`
}

type Metadata struct {
	ISOLanguageCode string `json:"iso_language_code"`
	ResultType      string `json:"result_type"`
}

type URLInfo struct {
	ExpandedURL any    `json:"expanded_url"`
	URL         string `json:"url"`
	Indices     []int  `json:"indices"`
}

type URLEntity struct {
	URLs []URLInfo `json:"urls"`
}

type Description struct {
	URLs []any `json:"urls"`
}

type UserEntities struct {
	URL         URLEntity   `json:"url"`
	Description Description `json:"description"`
}

type User struct {
	ProfileSidebarFillColor        string       `json:"profile_sidebar_fill_color"`
	ProfileSidebarBorderColor      string       `json:"profile_sidebar_border_color"`
	ProfileBackgroundTile          bool         `json:"profile_background_tile"`
	Name                           string       `json:"name"`
	ProfileImageURL                string       `json:"profile_image_url"`
	CreatedAt                      string       `json:"created_at"`
	Location                       string       `json:"location"`
	FollowRequestSent              any          `json:"follow_request_sent"`
	ProfileLinkColor               string       `json:"profile_link_color"`
	IsTranslator                   bool         `json:"is_translator"`
	IDStr                          string       `json:"id_str"`
	Entities                       UserEntities `json:"entities"`
	DefaultProfile                 bool         `json:"default_profile"`
	ContributorsEnabled            bool         `json:"contributors_enabled"`
	FavouritesCount                int          `json:"favourites_count"`
	URL                            any          `json:"url"`
	ProfileImageURLHTTPS           string       `json:"profile_image_url_https"`
	UTCOffset                      int          `json:"utc_offset"`
	ID                             int          `json:"id"`
	ProfileUseBackgroundImage      bool         `json:"profile_use_background_image"`
	ListedCount                    int          `json:"listed_count"`
	ProfileTextColor               string       `json:"profile_text_color"`
	Lang                           string       `json:"lang"`
	FollowersCount                 int          `json:"followers_count"`
	Protected                      bool         `json:"protected"`
	Notifications                  any          `json:"notifications"`
	ProfileBackgroundImageURLHTTPS string       `json:"profile_background_image_url_https"`
	ProfileBackgroundColor         string       `json:"profile_background_color"`
	Verified                       bool         `json:"verified"`
	GeoEnabled                     bool         `json:"geo_enabled"`
	TimeZone                       string       `json:"time_zone"`
	Description                    string       `json:"description"`
	DefaultProfileImage            bool         `json:"default_profile_image"`
	ProfileBackgroundImageURL      string       `json:"profile_background_image_url"`
	StatusesCount                  int          `json:"statuses_count"`
	FriendsCount                   int          `json:"friends_count"`
	Following                      any          `json:"following"`
	ShowAllInlineMedia             bool         `json:"show_all_inline_media"`
	ScreenName                     string       `json:"screen_name"`
}

type Status struct {
	Coordinates          any      `json:"coordinates"`
	Favorited            bool     `json:"favorited"`
	Truncated            bool     `json:"truncated"`
	CreatedAt            string   `json:"created_at"`
	IDStr                string   `json:"id_str"`
	Entities             Entities `json:"entities"`
	InReplyToUserIDStr   any      `json:"in_reply_to_user_id_str"`
	Contributors         any      `json:"contributors"`
	Text                 string   `json:"text"`
	Metadata             Metadata `json:"metadata"`
	RetweetCount         int      `json:"retweet_count"`
	InReplyToStatusIDStr any      `json:"in_reply_to_status_id_str"`
	ID                   int64    `json:"id"`
	Geo                  any      `json:"geo"`
	Retweeted            bool     `json:"retweeted"`
	InReplyToUserID      any      `json:"in_reply_to_user_id"`
	Place                any      `json:"place"`
	User                 User     `json:"user"`
	InReplyToScreenName  any      `json:"in_reply_to_screen_name"`
	Source               string   `json:"source"`
	InReplyToStatusID    any      `json:"in_reply_to_status_id"`
}

type SearchMetadata struct {
	MaxID       int64   `json:"max_id"`
	SinceID     int64   `json:"since_id"`
	RefreshURL  string  `json:"refresh_url"`
	NextResults string  `json:"next_results"`
	Count       int     `json:"count"`
	CompletedIn float64 `json:"completed_in"`
	SinceIDStr  string  `json:"since_id_str"`
	Query       string  `json:"query"`
	MaxIDStr    string  `json:"max_id_str"`
}

func mustDecode[T any](data []byte) T {
	var value T
	if err := stdjson.Unmarshal(data, &value); err != nil {
		panic(err)
	}
	return value
}

var (
	SmallGeneric  = mustDecode[any](SmallJSON)
	MediumValue   = mustDecode[TwitterResponse](MediumJSON)
	MediumGeneric = mustDecode[any](MediumJSON)
	LargeValue    = mustDecode[TwitterResponse](LargeJSON)
	LargeGeneric  = mustDecode[any](LargeJSON)
)
