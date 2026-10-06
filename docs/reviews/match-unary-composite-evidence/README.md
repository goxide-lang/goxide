# A04 evidence

[Publication ledger](testdata/publication-provenance.json) records original paths,
published paths, sizes and SHA-256 hashes. Listed raw evidence is copied verbatim.
The `testdata` directory preserves executable probe source without adding archival
Go packages to the repository's `./...` test set. Complete original logs remain at
the validation roots recorded in the ledger and the [delivery report](../match-unary-composite-20261006.md).

The preserved `acceptance.patch` contains unified-diff context indentation, and
the first three lines of `review-origin/review.md` use Markdown hard line breaks.
Their raw whitespace is retained. The publication diff check records these two
exact exclusions; authored documentation and all remaining files pass unchanged
Git whitespace checks.
