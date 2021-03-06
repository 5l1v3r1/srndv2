package frontend

import (
	"github.com/majestrate/srndv2/lib/config"
	"github.com/majestrate/srndv2/lib/database"
	"github.com/majestrate/srndv2/lib/model"
	"github.com/majestrate/srndv2/lib/nntp"
)

// a frontend that displays nntp posts and allows posting
type Frontend interface {

	// run mainloop
	Serve()

	// do we accept this inbound post?
	AllowPost(p model.PostReference) bool

	// trigger a manual regen of indexes for a root post
	Regen(p model.PostReference)

	// implements nntp.EventHooks
	GotArticle(msgid nntp.MessageID, group nntp.Newsgroup)

	// implements nntp.EventHooks
	SentArticleVia(msgid nntp.MessageID, feedname string)

	// reload config
	Reload(c *config.FrontendConfig)
}

// create a new http frontend give frontend config
func NewHTTPFrontend(c *config.FrontendConfig, db database.Database) (f Frontend, err error) {

	var mid Middleware
	if c.Middleware != nil {
		// middleware configured
		mid, err = OverchanMiddleware(c.Middleware, db)
	}

	if err == nil {
		// create http frontend only if no previous errors
		f, err = createHttpFrontend(c, mid, db)
	}
	return
}
