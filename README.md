Features
========
- [x] Intense performance.
- [x] Runs on Google AppEngine
- [x] WYSWIG editing of blog posts
- [x] Disqus integration
- [x] Ability to create arbitrary links
- [x] Support for themes
- [x] Ability to create arbitrary pages
- [ ] Per-date archiving
- [ ] Page caching to avoid Datastore hits


Getting Started
===============
1. Visit https://appengine.google.com/ and click 'Create Application'
2. Edit *app.yaml*, inserting your application id.

```yaml
application: your-app-id
```
3. Edit *verbalize.yml* to define your blog settings.

```yaml
title: SF to SD, or Bust!
subtitle: 700 miles of smiles.
description: Going the distance for AIDS/Lifecycle.

author: Thomas Stromberg
author_email: t+verbalize@stromberg.org

# For comments, you must register at http://disqus.com/
disqus_id: sf2sd
````

4. Start up a local server for testing.

```sh
/path/to/sdk/dev_appserver.py /path/to/site
```

5. Push to production when ready!

```sh
/path/to/sdk/appcfg.py update /path/to/site
````
