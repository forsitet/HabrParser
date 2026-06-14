package domain

import "time"

type Article struct {
	Title         string
	URL           string
	PublishedAt   time.Time
	Rating        int
	CommentsCount int
	AuthorName    string
	AuthorURL     string
	AuthorKarma   float64
	Hubs          []string
	Tags          []string
	Text          string
	HasCodeBlocks bool
}

type Category struct {
	Alias string
	Title string
}

type UserSettings struct {
	TelegramUserID  int64
	Categories      []string
	AutoSendEnabled bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type SentArticle struct {
	TelegramUserID int64
	ArticleURL     string
	SentAt         time.Time
}

type DigestMessage struct {
	Article  Article
	Score    int
	Summary  string
	Hashtags []string
	HTML     string
}

type DigestStats struct {
	TargetDate     time.Time
	ArticlesFound  int
	ArticlesPassed int
	ArticlesSent   int
}

type DigestResult struct {
	Messages []DigestMessage
	Stats    DigestStats
}
