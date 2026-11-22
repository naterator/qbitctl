"""Second deterministic qBittorrent search plugin for provider selection tests."""

# VERSION: 1.0
# AUTHORS: qbitctl maintainers

import os

import helpers
from novaprinter import prettyPrinter


class qbitctl_fixture_alt:
    """Return results whose names and torrent URL identify this provider."""

    name = "Qbitctl Fixture Alt"
    supported_categories = {"all": None}
    _base_url = os.environ.get("QBITCTL_FIXTURE_BASE", "http://127.0.0.1:9").rstrip("/")
    # qBittorrent attributes each result to the plugin whose siteUrl exactly
    # matches engine_url. Keep this provider's site URL distinct from the
    # primary fixture even though both are served by the same test server.
    url = os.environ.get("QBITCTL_FIXTURE_ALT_BASE", _base_url + "/alt").rstrip("/")

    def search(self, what, cat="all"):
        del what, cat
        base = self.url
        for result in (
            {
                "link": "magnet:?xt=urn:btih:89abcdef0123456789abcdef0123456789abcdef&dn=qbitctl-alt-search-magnet",
                "name": "qbitctl alt search magnet",
                "size": "1 MiB",
                "seeds": 11,
                "leech": 3,
            },
            {
                "link": f"{base}/qbitctl-search-alt.torrent",
                "name": "qbitctl alt search torrent",
                "size": "1 B",
                "seeds": 9,
                "leech": 2,
            },
        ):
            result.update(
                {
                    "engine_url": base,
                    "desc_link": f"{base}/qbitctl-search-alt-description",
                    "pub_date": -1,
                }
            )
            prettyPrinter(result)

    def download_torrent(self, download_url):
        if download_url.startswith("magnet:"):
            print(f"{download_url} {download_url}")
            return
        if not download_url.endswith("/qbitctl-search-alt.torrent"):
            raise ValueError("qbitctl alternate fixture received another provider URL")
        print(helpers.download_file(download_url))
