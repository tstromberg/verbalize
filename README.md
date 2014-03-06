Features
========
- Runs on Google AppEngine, and is efficient enough to run within free quota
- Designed for high-performance, availability, and scalability
- Utilizes in-memory caching for all page loads
- WYSWIG editing of blog posts
- Disqus-powered comment system
- Able to create arbitrary pages and links
- Basic support for themes
- Able to extract, cache, and redisplay contents from other websites

Missing Features
================
- Auto-save of drafts

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
