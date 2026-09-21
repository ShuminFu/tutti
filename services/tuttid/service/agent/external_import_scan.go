package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tutti-os/tutti/packages/agent/daemon/providerregistry"
	"github.com/tutti-os/tutti/services/tuttid/data/externalimportcatalog"
)

type externalScanOptions struct {
	keepBodies bool
	sessionIDs map[string]struct{}
}

type discoveredExternalFile struct {
	seq        int
	provider   string
	root       string
	path       string
	relPath    string
	descriptor providerregistry.ProviderDescriptor
	signature  externalimportcatalog.Signature
	key        externalimportcatalog.Key
	// grokSessionDir marks a unit whose path is a Grok session directory
	// rather than a transcript file, so it needs the directory parser and a
	// folded signature instead of one file's stat.
	grokSessionDir bool
}

type parsedExternalFile struct {
	file        discoveredExternalFile
	session     externalImportedSession
	ok          bool
	empty       bool
	err         error
	catalogHit  bool
	parsedBytes int64
}

func scanExternalAgentSessions(
	ctx context.Context,
	providers []string,
	days int,
	archivePath string,
	archiveKind string,
) (externalScanData, error) {
	return (*Service)(nil).scanExternalAgentSessions(ctx, providers, days, archivePath, archiveKind, externalScanOptions{})
}

func (s *Service) scanExternalAgentSessions(
	ctx context.Context,
	providers []string,
	days int,
	archivePath string,
	archiveKind string,
	opts externalScanOptions,
) (externalScanData, error) {
	started := time.Now()
	if strings.TrimSpace(archivePath) != "" {
		data, err := s.scanExternalArchiveSessions(ctx, providers, days, archivePath, archiveKind, opts)
		if err != nil {
			return externalScanData{}, err
		}
		finishExternalScanDiagnostics(&data, started)
		return data, nil
	}
	data, err := s.scanExternalLocalSessions(ctx, providers, days, opts)
	if err != nil {
		return externalScanData{}, err
	}
	finishExternalScanDiagnostics(&data, started)
	return data, nil
}

func (s *Service) scanExternalArchiveSessions(
	ctx context.Context,
	providers []string,
	days int,
	archivePath string,
	archiveKind string,
	opts externalScanOptions,
) (externalScanData, error) {
	archiveKind = normalizeExternalImportArchiveKind(archiveKind)
	if archiveKind == ExternalImportArchiveKindClaude &&
		len(providers) > 0 && !providersIncludeArchiveImportParser(providers) {
		return externalScanData{}, fmt.Errorf(
			"%w: a Claude export archive scan requires the claude-code provider",
			ErrInvalidArgument,
		)
	}
	cutoffUnixMS := int64(0)
	if days > 0 {
		cutoffUnixMS = externalScanCutoffUnixMS(days)
	}
	var (
		data externalScanData
		err  error
	)
	switch archiveKind {
	case ExternalImportArchiveKindChatGPT:
		data, err = scanChatGPTExportArchive(ctx, archivePath, cutoffUnixMS, opts)
	default:
		data, err = scanClaudeExportArchive(ctx, archivePath, cutoffUnixMS, opts)
	}
	if err != nil {
		return externalScanData{}, err
	}
	nowMS := time.Now().UnixMilli()
	data.result.ScannedAtUnixMS = nowMS
	data.result.CutoffUnixMS = cutoffUnixMS
	data.result.Complete = true
	return data, nil
}

func (s *Service) scanExternalLocalSessions(
	ctx context.Context,
	providers []string,
	days int,
	opts externalScanOptions,
) (externalScanData, error) {
	now := time.Now()
	cutoffUnixMS := externalScanCutoffUnixMS(days)
	generation := now.UnixNano()
	discoverStarted := time.Now()
	files, providerRoots, completedRoots, walkErrors, err := discoverExternalImportFiles(ctx, providers)
	if err != nil {
		return externalScanData{}, err
	}
	discoverMS := time.Since(discoverStarted).Milliseconds()
	if err := ctx.Err(); err != nil {
		return externalScanData{}, err
	}

	parsed, catalogHits, parsedFiles, parsedBytes, titleQueries, parseMS, titleMS, err := s.loadExternalImportSummaries(ctx, files, completedRoots, generation, opts.keepBodies)
	if err != nil {
		return externalScanData{}, err
	}
	if err := ctx.Err(); err != nil {
		return externalScanData{}, err
	}

	cwdMemo := map[string]resolvedExternalImportCwd{}
	sessionsByProvider := map[string]*providerSessionAcc{}
	var filtered int
	for _, item := range parsed {
		if item.err != nil || !item.ok {
			continue
		}
		session := item.session
		reprojectExternalImportSession(&session, cwdMemo, now)
		if !externalScanSessionIDAllowed(opts.sessionIDs, session) {
			continue
		}
		if session.UpdatedAtUnixMS < cutoffUnixMS {
			filtered++
			continue
		}
		acc := sessionsByProvider[session.Provider]
		if acc == nil {
			acc = &providerSessionAcc{indexBySessionID: map[string]int{}}
			sessionsByProvider[session.Provider] = acc
		}
		if index, exists := acc.indexBySessionID[session.ProviderSessionID]; exists {
			if session.UpdatedAtUnixMS > acc.sessions[index].UpdatedAtUnixMS {
				acc.sessions[index] = session
			}
			continue
		}
		acc.indexBySessionID[session.ProviderSessionID] = len(acc.sessions)
		acc.sessions = append(acc.sessions, session)
	}

	data := externalScanData{}
	data.result.Errors = append(data.result.Errors, walkErrors...)
	for _, item := range parsed {
		if item.err != nil {
			data.result.Errors = append(data.result.Errors, ExternalImportError{
				Provider:   item.file.provider,
				SourcePath: item.file.path,
				Message:    item.err.Error(),
			})
		}
	}
	projects := map[string]*ExternalImportProject{}
	for _, provider := range normalizeExternalImportProviders(providers) {
		summary := ExternalImportProvider{Provider: provider}
		if roots := providerRoots[provider]; len(roots) > 0 {
			summary.Root = roots[0]
			if _, err := os.Stat(roots[0]); err == nil {
				summary.Available = true
			}
		}
		acc := sessionsByProvider[provider]
		if acc != nil {
			for _, session := range acc.sessions {
				project, ok := projectFromExternalSession(session)
				if !ok {
					data.result.SkippedSessions++
					continue
				}
				if opts.keepBodies {
					data.sessions = append(data.sessions, session)
				} else {
					summarySession := session
					dropExternalSessionBodies(&summarySession)
					data.sessions = append(data.sessions, summarySession)
				}
				data.result.ScannedSessions++
				data.result.ScannedMessages += externalImportMessageCount(session)
				data.result.Sessions = append(data.result.Sessions, externalImportSessionSummary(session, project.Path))
				upsertExternalImportProject(projects, project, session.Provider)
				summary.SessionCount++
				summary.MessageCount += externalImportMessageCount(session)
			}
		}
		for _, failure := range walkErrors {
			if failure.Provider == provider && strings.TrimSpace(summary.Error) == "" {
				summary.Error = failure.Message
			}
		}
		data.result.Providers = append(data.result.Providers, summary)
	}
	for _, project := range projects {
		sort.Strings(project.Providers)
		data.result.Projects = append(data.result.Projects, *project)
	}
	sortExternalImportResult(&data)
	data.result.ScannedAtUnixMS = now.UnixMilli()
	data.result.CutoffUnixMS = cutoffUnixMS
	data.result.Complete = true
	data.result.Diagnostics = ExternalImportScanDiagnostics{
		DiscoveredFiles:  len(files),
		StatFiles:        len(files),
		CatalogHits:      catalogHits,
		ParsedFiles:      parsedFiles,
		ParsedBytes:      parsedBytes,
		FilteredSessions: filtered,
		TitleQueries:     titleQueries,
		DiscoverMS:       discoverMS,
		ParseMS:          parseMS,
		TitleMS:          titleMS,
	}
	return data, ctx.Err()
}

type providerSessionAcc struct {
	sessions         []externalImportedSession
	indexBySessionID map[string]int
}

type resolvedExternalImportCwd struct {
	cwd       string
	noProject bool
	ok        bool
}

func (s *Service) loadExternalImportSummaries(
	ctx context.Context,
	files []discoveredExternalFile,
	completedRoots []discoveredExternalFile,
	generation int64,
	keepBodies bool,
) ([]parsedExternalFile, int, int, int64, int, int64, int64, error) {
	results := make([]parsedExternalFile, 0, len(files))
	if len(files) == 0 {
		if s != nil && s.ExternalImportCatalog != nil && !keepBodies {
			for _, root := range completedRoots {
				if err := s.ExternalImportCatalog.CompleteRoot(ctx, root.provider, root.root, generation, nil); err != nil {
					slog.Warn("external import catalog complete-root failed", "provider", root.provider, "root", root.root, "error", err)
				}
			}
		}
		return results, 0, 0, 0, 0, 0, 0, nil
	}
	keys := make([]externalimportcatalog.Key, 0, len(files))
	for _, file := range files {
		keys = append(keys, file.key)
	}
	cached := map[externalimportcatalog.Key]externalimportcatalog.Entry{}
	if s != nil && s.ExternalImportCatalog != nil && !keepBodies {
		lookedUp, err := s.ExternalImportCatalog.Lookup(ctx, keys)
		if err != nil {
			slog.Warn("external import catalog lookup failed; scanning live", "error", err)
		} else {
			cached = lookedUp
		}
	}

	toParse := make([]discoveredExternalFile, 0, len(files))
	catalogHits := 0
	for _, file := range files {
		entry, ok := cached[file.key]
		if ok && externalimportcatalog.SignaturesEqual(entry.Signature, file.signature) {
			catalogHits++
			parsed := parsedExternalFile{file: file, catalogHit: true}
			if entry.Summary.Valid {
				parsed.ok = true
				parsed.session = sessionFromCatalogEntry(file, entry)
			} else {
				parsed.empty = entry.Summary.Empty
			}
			results = append(results, parsed)
			continue
		}
		toParse = append(toParse, file)
	}

	parseStarted := time.Now()
	parsedResults := runBoundedIndexed(ctx, s.externalImportConcurrency(), len(toParse), func(ctx context.Context, index int) parsedExternalFile {
		file := toParse[index]
		parsed := parsedExternalFile{file: file, parsedBytes: file.signature.Size}
		session, ok, err := parseExternalImportUnit(ctx, file)
		if err != nil {
			parsed.err = err
			return parsed
		}
		if !ok {
			parsed.empty = true
			return parsed
		}
		after, err := inspectExternalImportUnit(file)
		if err != nil {
			parsed.err = err
			return parsed
		}
		if !externalimportcatalog.ContentSignaturesEqual(file.signature, after) {
			parsed.err = fmt.Errorf("source file changed while reading")
			return parsed
		}
		parsed.ok = true
		parsed.session = session
		return parsed
	})
	if err := ctx.Err(); err != nil {
		return nil, 0, 0, 0, 0, 0, 0, err
	}
	parseMS := time.Since(parseStarted).Milliseconds()

	parsedFiles := 0
	var parsedBytes int64
	upserts := make([]externalimportcatalog.Entry, 0, len(parsedResults))
	liveByRoot := map[string][]externalimportcatalog.Key{}
	rootMeta := map[string]discoveredExternalFile{}
	for _, root := range completedRoots {
		id := root.provider + "\x00" + root.root
		rootMeta[id] = root
		if _, ok := liveByRoot[id]; !ok {
			liveByRoot[id] = []externalimportcatalog.Key{}
		}
	}
	for _, file := range files {
		id := file.provider + "\x00" + file.root
		liveByRoot[id] = append(liveByRoot[id], file.key)
		rootMeta[id] = file
	}
	for _, item := range parsedResults {
		parsedFiles++
		parsedBytes += item.parsedBytes
		results = append(results, item)
		if item.err != nil {
			continue
		}
		upserts = append(upserts, catalogEntryFromParsed(item, generation))
	}
	sort.SliceStable(results, func(left, right int) bool {
		return results[left].file.seq < results[right].file.seq
	})
	if s != nil && s.ExternalImportCatalog != nil && !keepBodies {
		if err := s.ExternalImportCatalog.Upsert(ctx, upserts); err != nil {
			slog.Warn("external import catalog upsert failed", "error", err)
		} else {
			for id, live := range liveByRoot {
				file := rootMeta[id]
				if err := s.ExternalImportCatalog.CompleteRoot(ctx, file.provider, file.root, generation, live); err != nil {
					slog.Warn("external import catalog complete-root failed", "provider", file.provider, "root", file.root, "error", err)
				}
			}
		}
	}
	titleStarted := time.Now()
	titleQueries := s.applyCodexTitleOverlay(ctx, results)
	titleMS := time.Since(titleStarted).Milliseconds()
	return results, catalogHits, parsedFiles, parsedBytes, titleQueries, parseMS, titleMS, nil
}

func (s *Service) applyCodexTitleOverlay(ctx context.Context, results []parsedExternalFile) int {
	titlesByRoot := map[string]map[string]string{}
	queries := 0
	for i := range results {
		item := &results[i]
		if !item.ok {
			continue
		}
		if item.file.descriptor.ExternalImport.TitleCatalogKind != providerregistry.ExternalImportTitleCatalogKindCodexSQLite {
			continue
		}
		titles, queried := titlesByRoot[item.file.root]
		if !queried {
			titles = s.codexThreadTitlesCached(ctx, item.file.root)
			titlesByRoot[item.file.root] = titles
			queries++
		}
		if title := strings.TrimSpace(titles[item.session.ProviderSessionID]); title != "" {
			item.session.Title = truncateExternalTitle(title)
		}
	}
	return queries
}

func discoverExternalImportFiles(ctx context.Context, providers []string) ([]discoveredExternalFile, map[string][]string, []discoveredExternalFile, []ExternalImportError, error) {
	files := make([]discoveredExternalFile, 0)
	completedRoots := make([]discoveredExternalFile, 0)
	providerRoots := map[string][]string{}
	errors := make([]ExternalImportError, 0)
	seq := 0
	for _, provider := range normalizeExternalImportProviders(providers) {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, nil, err
		}
		if provider == grokImportProvider {
			grokFiles, grokRoots, grokCompleted, grokErrors := discoverGrokSessionDirs(ctx, &seq)
			providerRoots[provider] = grokRoots
			files = append(files, grokFiles...)
			completedRoots = append(completedRoots, grokCompleted...)
			errors = append(errors, grokErrors...)
			continue
		}
		descriptor, ok := providerregistry.Find(provider)
		if !ok || !descriptor.ExternalImport.Enabled {
			continue
		}
		roots := externalProviderRoots(descriptor.ExternalImport)
		providerRoots[provider] = roots
		descSig := externalImportDescriptorSignature(descriptor.ExternalImport)
		for _, root := range roots {
			if err := ctx.Err(); err != nil {
				return nil, nil, nil, nil, err
			}
			if info, err := os.Stat(root); err != nil || !info.IsDir() {
				continue
			}
			found, walkErr := externalProviderJSONLFiles(ctx, descriptor.ExternalImport, root)
			if walkErr != nil {
				errors = append(errors, ExternalImportError{Provider: provider, Message: walkErr.Error()})
				continue
			}
			completedRoots = append(completedRoots, discoveredExternalFile{
				provider:   provider,
				root:       root,
				descriptor: descriptor,
			})
			for _, path := range found {
				if err := ctx.Err(); err != nil {
					return nil, nil, nil, nil, err
				}
				sig, err := externalimportcatalog.InspectPath(path)
				if err != nil {
					errors = append(errors, ExternalImportError{Provider: provider, SourcePath: path, Message: err.Error()})
					continue
				}
				sig.DescriptorSig = descSig
				rel, err := filepath.Rel(root, path)
				if err != nil {
					rel = path
				}
				rel = filepath.ToSlash(rel)
				seq++
				files = append(files, discoveredExternalFile{
					seq:        seq,
					provider:   provider,
					root:       root,
					path:       path,
					relPath:    rel,
					descriptor: descriptor,
					signature:  sig,
					key: externalimportcatalog.Key{
						Provider: provider,
						Root:     root,
						RelPath:  rel,
					},
				})
			}
		}
	}
	return files, providerRoots, completedRoots, errors, nil
}

func externalProviderJSONLFiles(ctx context.Context, descriptor providerregistry.ExternalImportDescriptor, root string) ([]string, error) {
	roots := make([]string, 0, len(descriptor.ScanDirectories))
	for _, directory := range descriptor.ScanDirectories {
		roots = append(roots, filepath.Join(root, directory))
	}
	files := make([]string, 0)
	for _, scanRoot := range roots {
		if info, err := os.Stat(scanRoot); err != nil || !info.IsDir() {
			continue
		}
		err := filepath.WalkDir(scanRoot, func(path string, entry os.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				return err
			}
			if entry.IsDir() {
				for _, prefix := range descriptor.SkipDirectoryPrefixes {
					if strings.HasPrefix(entry.Name(), prefix) {
						return filepath.SkipDir
					}
				}
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				if _, statErr := os.Stat(path); statErr != nil {
					return nil
				}
			}
			if strings.EqualFold(filepath.Ext(path), ".jsonl") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	return files, nil
}

func parseExternalProviderJSONL(
	ctx context.Context,
	descriptor providerregistry.ProviderDescriptor,
	path string,
) (externalImportedSession, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return externalImportedSession{}, false, err
	}
	defer file.Close()
	var session externalImportedSession
	var ok bool
	switch descriptor.ExternalImport.ParserKind {
	case providerregistry.ExternalImportParserKindCodexJSONL:
		session, ok, err = parseCodexJSONL(ctx, path, file)
	case providerregistry.ExternalImportParserKindClaudeJSONL:
		session, ok, err = parseClaudeCodeJSONL(ctx, path, file)
	default:
		return externalImportedSession{}, false, fmt.Errorf("external import parser %q is unsupported", descriptor.ExternalImport.ParserKind)
	}
	if ok {
		session.Provider = descriptor.Identity.ID
	}
	return session, ok, err
}

func sessionFromCatalogEntry(file discoveredExternalFile, entry externalimportcatalog.Entry) externalImportedSession {
	session := externalImportedSession{
		Provider:          file.provider,
		ProviderSessionID: entry.Summary.SessionID,
		SourcePath:        file.path,
		RawCwd:            entry.Summary.RawCwd,
		Cwd:               entry.Summary.RawCwd,
		Title:             entry.Summary.Title,
		SummaryTitle:      entry.Summary.SummaryTitle,
		MessageCount:      entry.Summary.MessageCount,
		StartedAtUnixMS:   entry.Summary.StartedAtUnixMS,
		UpdatedAtUnixMS:   entry.Summary.UpdatedAtUnixMS,
		TimeSource:        entry.Summary.TimeSource,
		Model:             entry.Summary.Model,
		ReasoningEffort:   entry.Summary.Effort,
		ResumeSupported:   entry.Summary.ResumeSupported,
	}
	return session
}

func catalogEntryFromParsed(item parsedExternalFile, generation int64) externalimportcatalog.Entry {
	summary := externalimportcatalog.Summary{
		Valid: item.ok,
		Empty: item.empty || !item.ok,
	}
	if item.ok {
		summary.SessionID = item.session.ProviderSessionID
		summary.RawCwd = firstNonEmptyString(item.session.RawCwd, item.session.Cwd)
		summary.Title = item.session.Title
		summary.SummaryTitle = item.session.SummaryTitle
		summary.MessageCount = externalImportMessageCount(item.session)
		summary.StartedAtUnixMS = item.session.StartedAtUnixMS
		summary.UpdatedAtUnixMS = item.session.UpdatedAtUnixMS
		summary.TimeSource = item.session.TimeSource
		summary.Model = item.session.Model
		summary.Effort = item.session.ReasoningEffort
		summary.ResumeSupported = item.session.ResumeSupported
	}
	return externalimportcatalog.Entry{
		Key:        item.file.key,
		Signature:  item.file.signature,
		Summary:    summary,
		Generation: generation,
	}
}

func reprojectExternalImportSession(session *externalImportedSession, memo map[string]resolvedExternalImportCwd, now time.Time) {
	if session == nil {
		return
	}
	refreshExternalSessionFallbackTime(session, now)
	raw := firstNonEmptyString(session.RawCwd, session.Cwd)
	if cached, ok := memo[raw]; ok {
		if cached.ok {
			session.Cwd = cached.cwd
			session.NoProject = cached.noProject
		}
		return
	}
	cwd, ok := resolveExternalImportSessionCwd(raw)
	resolved := resolvedExternalImportCwd{ok: ok, cwd: cwd}
	if ok {
		resolved.noProject = isExternalImportNoProjectCwd(session.Provider, cwd)
		session.Cwd = cwd
		session.NoProject = resolved.noProject
		session.RawCwd = raw
	}
	memo[raw] = resolved
}

// parseExternalImportUnit parses one discovered unit into a session summary.
// Most providers keep one session per JSONL file; Grok keeps one per session
// directory, so that unit needs its own parser.
func parseExternalImportUnit(ctx context.Context, file discoveredExternalFile) (externalImportedSession, bool, error) {
	if file.grokSessionDir {
		return parseGrokSessionDir(ctx, file.path)
	}
	return parseExternalProviderJSONL(ctx, file.descriptor, file.path)
}

// inspectExternalImportUnit re-reads a unit's signature. It must match what
// discovery recorded, or the post-parse change check would compare a folded
// directory signature against a single stat and always report a change.
func inspectExternalImportUnit(file discoveredExternalFile) (externalimportcatalog.Signature, error) {
	if file.grokSessionDir {
		return grokSessionDirSignature(file.path)
	}
	return externalimportcatalog.InspectPath(file.path)
}

func externalImportDescriptorSignature(descriptor providerregistry.ExternalImportDescriptor) string {
	return strings.Join([]string{
		string(descriptor.ParserKind),
		string(descriptor.TitleCatalogKind),
		string(descriptor.UserTextCleanerKind),
		strings.Join(descriptor.ScanDirectories, ","),
		strings.Join(descriptor.SkipDirectoryPrefixes, ","),
		descriptor.NoProjectHomeRelativeDir,
		strconv.Itoa(externalimportcatalog.ParserVersion),
	}, "|")
}

func externalScanSessionIDAllowed(filter map[string]struct{}, session externalImportedSession) bool {
	if len(filter) == 0 {
		return true
	}
	_, ok := filter[externalImportedSessionID(session.Provider, session.ProviderSessionID)]
	return ok
}

func sortExternalImportResult(data *externalScanData) {
	sort.SliceStable(data.result.Projects, func(left, right int) bool {
		if data.result.Projects[left].LastUpdatedAtUnixMS == data.result.Projects[right].LastUpdatedAtUnixMS {
			return data.result.Projects[left].Path < data.result.Projects[right].Path
		}
		return data.result.Projects[left].LastUpdatedAtUnixMS > data.result.Projects[right].LastUpdatedAtUnixMS
	})
	sort.SliceStable(data.result.Sessions, func(left, right int) bool {
		if data.result.Sessions[left].LastUpdatedAtUnixMS == data.result.Sessions[right].LastUpdatedAtUnixMS {
			return data.result.Sessions[left].ID < data.result.Sessions[right].ID
		}
		return data.result.Sessions[left].LastUpdatedAtUnixMS > data.result.Sessions[right].LastUpdatedAtUnixMS
	})
}

func finishExternalScanDiagnostics(data *externalScanData, started time.Time) {
	if data == nil {
		return
	}
	data.result.Diagnostics.TotalMS = time.Since(started).Milliseconds()
	slog.Info("external import scan",
		"discoveredFiles", data.result.Diagnostics.DiscoveredFiles,
		"statFiles", data.result.Diagnostics.StatFiles,
		"catalogHits", data.result.Diagnostics.CatalogHits,
		"parsedFiles", data.result.Diagnostics.ParsedFiles,
		"parsedBytes", data.result.Diagnostics.ParsedBytes,
		"filteredSessions", data.result.Diagnostics.FilteredSessions,
		"titleQueries", data.result.Diagnostics.TitleQueries,
		"scannedSessions", data.result.ScannedSessions,
		"scannedMessages", data.result.ScannedMessages,
		"discoverMS", data.result.Diagnostics.DiscoverMS,
		"parseMS", data.result.Diagnostics.ParseMS,
		"titleMS", data.result.Diagnostics.TitleMS,
		"totalMS", data.result.Diagnostics.TotalMS,
	)
}
