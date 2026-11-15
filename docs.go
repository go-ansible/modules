package modules

// docsMerge combines the generated per-letter doc tables into the one
// map callers use. It is hand-written rather than generated so that
// the generated files hold nothing but data.
//
// A name appearing in two shards would be silently overwritten here.
// That cannot happen while each shard is keyed by its own entries'
// initial letter, and TestDocsShardsDoNotOverlap asserts it anyway,
// because "cannot happen by construction" is a property of the
// generator, not of the files that are committed.
func docsMerge(shards ...map[string]string) map[string]string {
	total := 0
	for _, shard := range shards {
		total += len(shard)
	}
	all := make(map[string]string, total)
	for _, shard := range shards {
		for name, doc := range shard {
			all[name] = doc
		}
	}
	return all
}
