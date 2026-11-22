"""Deterministic qBittorrent search plugin used by integration tests.

The integration harness sets ``QBITCTL_FIXTURE_BASE`` in the qBittorrent
container. Keeping the fixture URL in the environment lets one checked-in
plugin serve every test run without embedding a runner-specific port.
"""

# VERSION: 1.0
# AUTHORS: qbitctl maintainers

import os

import helpers
from novaprinter import prettyPrinter


class qbitctl_fixture:
    """Return one magnet and one torrent URL for the compatibility suite."""

    name = "Qbitctl Fixture"
    supported_categories = {"all": None}
    url = os.environ.get("QBITCTL_FIXTURE_BASE", "http://127.0.0.1:9").rstrip("/")

    def search(self, what, cat="all"):
        del what, cat
        base = self.url
        for result in (
            {
                "link": "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=qbitctl-search-magnet",
                "name": "qbitctl search magnet",
                "size": "1 MiB",
                "seeds": 7,
                "leech": 2,
            },
            {
                "link": f"{base}/qbitctl-search.torrent",
                "name": "qbitctl search torrent",
                "size": "1 B",
                "seeds": 5,
                "leech": 1,
            },
        ):
            result.update(
                {
                    "engine_url": base,
                    "desc_link": f"{base}/qbitctl-search-description",
                    "pub_date": -1,
                }
            )
            prettyPrinter(result)

    def download_torrent(self, download_url):
        if download_url.startswith("magnet:"):
            print(f"{download_url} {download_url}")
            return
        if not download_url.endswith("/qbitctl-search.torrent"):
            raise ValueError("qbitctl fixture received a torrent URL from another provider")
        print(helpers.download_file(download_url))
