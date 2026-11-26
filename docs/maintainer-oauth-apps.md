# Platform setup

Every platform needs a developer app that you register yourself. Its credentials go in `config.toml`, which
`social-mcp config path` locates. After editing the config, restart the daemon with `social-mcp daemon stop`
followed by `social-mcp daemon start`, then connect accounts with `social-mcp account add <platform>`.

The examples assume the default `server.listen_port = 8765`. If you change the port, change it in every
redirect URI below too.

## YouTube

1. In the Google Cloud console, create a project and enable **YouTube Data API v3**.
2. Configure the **OAuth consent screen**. Add the scopes `youtube.upload` and `youtube.readonly`.
3. Set the consent screen's publishing status to **In production**. In "Testing" status, logins expire after
   7 days. An unverified app shows a warning screen when you sign in, which is fine for your own use.
4. Create an **OAuth client ID** of type **Desktop app**. Desktop clients accept loopback redirects such as
   `http://127.0.0.1:8765/oauth/callback` without registering them.
5. Copy the client ID and secret into the config:

   ```toml
   [youtube]
   client_id = "....apps.googleusercontent.com"
   client_secret = "..."
   ```

6. Run `social-mcp account add youtube`. On the consent screen, pick the channel to connect. Run the command
   again for each additional channel or brand account.

Each Google Cloud project can upload up to 100 videos per day.

## TikTok

1. On developers.tiktok.com, create an app and add the **Login Kit** and **Content Posting API** products.
2. Under Login Kit, choose the **Desktop** platform and add the redirect URI
   `http://127.0.0.1:8765/oauth/callback`. TikTok's desktop rules also accept a wildcard port such as
   `http://127.0.0.1:*/oauth/callback`.
3. Request the scopes `user.info.basic` and `video.upload`.
4. Until TikTok approves the app, use a **sandbox**. Add your own TikTok accounts as target users; a sandbox
   allows up to 10.
5. Copy the client key and secret into the config:

   ```toml
   [tiktok]
   client_key = "..."
   client_secret = "..."
   ```

6. Run `social-mcp account add tiktok`.

Uploads go to the creator's TikTok inbox as drafts. Open the TikTok notification to add the caption, choose
privacy and post. TikTok allows at most 5 pending drafts per 24 hours.

Whether sandbox target users can receive inbox uploads has not been verified yet.

## Meta (Facebook Pages and Instagram Reels)

Both platforms use one Meta app.

1. On developers.facebook.com, create an app of type **Business** and add **Facebook Login for Business**.
2. Create a **Login configuration** with a **user access token** that grants:
   - `pages_show_list`
   - `pages_read_engagement`
   - `pages_manage_posts` (Facebook Pages)
   - `instagram_basic` and `instagram_content_publish` (Instagram)

   Note its **configuration ID**.
3. In Facebook Login for Business settings, add `http://localhost:8765/oauth/callback` to **Valid OAuth
   Redirect URIs**. Meta matches the URI exactly.
4. For Instagram, the account must be a professional account (Business or Creator) linked to one of your
   Facebook Pages.
5. Copy the values into the config:

   ```toml
   [meta]
   app_id = "..."
   app_secret = "..."
   login_config_id = "..."
   graph_version = "v26.0"
   app_mode = "development"   # set to "live" once the app is in Live mode
   ```

6. Run `social-mcp account add facebook` to connect every Page where you can create content, and
   `social-mcp account add instagram` to connect the linked Instagram accounts.

### App modes and visibility

- **Development:** only people with a role on the app (admins, developers, testers) can see posts the app
  creates. `list_accounts` reports this as the `visible_to_app_roles_only` restriction.
- **Live:** posts are public. Meta documents App Review, plus Business Verification for Advanced Access, as
  the route to Live mode. Whether a Business app can go Live with only Standard Access, used only by people
  with roles on it, and still produce public posts has **not been verified**. Test it with one post before
  relying on it.
- Meta enforces HTTPS for OAuth redirects. Whether it accepts the `http://localhost` redirect in Live mode
  has **not been verified**. If sign-in fails after switching to Live, the redirect is the likely cause.

### Token lifetimes

- **Facebook:** each Page uses its own Page token, which does not expire. Facebook accounts stay connected
  until you remove the app or change your password.
- **Instagram:** uses your long-lived user token, which expires after about 60 days. `list_accounts` shows
  `expiring` in the last 7 days; run `social-mcp account reauth <id>` then.
