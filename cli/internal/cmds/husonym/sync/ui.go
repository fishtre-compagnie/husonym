package sync_cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	syncmap "sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/fishtre-compagnie/husonym/cli/internal/output"
	benthosbuilder "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder"
	"github.com/redpanda-data/benthos/v4/public/service"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type model struct {
	ctx              context.Context
	logger           *slog.Logger
	benv             *service.Environment
	groupedConfigs   [][]*benthosbuilder.BenthosConfigResponse
	tableSynced      int
	index            int
	width            int
	height           int
	spinner          spinner.Model
	done             bool
	totalConfigCount int
	outputType       output.OutputType
	// err is the table that failed, which ended the sync before its last one.
	err error
	// cancel stops the tables being synced, and inFlight tells when they have: a sync that
	// ends stops them and waits for them, endWait at most.
	cancel   context.CancelFunc
	inFlight inFlightGroups
	endWait  time.Duration
	// stop carries the critical errors of the streams: one stops the sync. The watch reads
	// it, and answers on askStop the groups of tables that ask whether one came.
	stop    <-chan error
	askStop chan chan error
}

// inFlightGroups counts the groups of tables being synced, until the sync ends: a group that
// would start after that does not.
type inFlightGroups struct {
	mu      syncmap.Mutex
	ended   bool
	running syncmap.WaitGroup
}

// begin counts a group as being synced. It answers false once the sync has ended: the group
// must not start.
func (g *inFlightGroups) begin() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.ended {
		return false
	}
	g.running.Add(1)
	return true
}

func (g *inFlightGroups) done() { g.running.Done() }

// end keeps any more group from starting, and waits for those being synced, for a time: it
// says whether they were done by then.
func (g *inFlightGroups) end(timeout time.Duration) bool {
	g.mu.Lock()
	g.ended = true
	g.mu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		g.running.Wait()
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

var (
	bold                = lipgloss.NewStyle().PaddingLeft(2).Bold(true)
	printlog            = lipgloss.NewStyle().PaddingLeft(2)
	currentPkgNameStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("211"))
	doneStyle           = lipgloss.NewStyle().Margin(1, 2)
	checkMark           = lipgloss.NewStyle().
				PaddingLeft(2).
				Foreground(lipgloss.Color("42")).
				SetString("✓")
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Margin(1, 0)
	dotStyle      = helpStyle.UnsetMargins()
	durationStyle = dotStyle
)

func newModel(
	ctx context.Context,
	benv *service.Environment,
	groupedConfigs [][]*benthosbuilder.BenthosConfigResponse,
	logger *slog.Logger,
	outputType output.OutputType,
) *model {
	s := spinner.New()
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("63"))
	// The sync has a context of its own: a key or its end stops the tables being synced.
	ctx, cancel := context.WithCancel(ctx)
	return &model{
		ctx:              ctx,
		cancel:           cancel,
		groupedConfigs:   groupedConfigs,
		tableSynced:      0,
		spinner:          s,
		totalConfigCount: getConfigCount(groupedConfigs),
		endWait:          syncEndWait,
		askStop:          make(chan chan error),
		logger:           logger,
		outputType:       outputType,
		benv:             benv,
	}
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.syncConfigs(m.ctx, m.groupedConfigs[m.index]), m.spinner.Tick)
}

// watchStop waits for a critical error of a stream, until the sync ends, and tells the program
// of it. It returns what it heard, on a channel closed once the watch has ended: a program that
// ends meanwhile is told nothing, and the error is no less the reason the sync failed.
func (m *model) watchStop(tell func(tea.Msg)) <-chan error {
	heard := make(chan error, 1)
	// Stopped here, without waiting for the program: the stream goes on past the row it was
	// refused, and the tables still queued would start meanwhile.
	stopOn := func(err error) {
		heard <- err
		m.cancel()
		tell(syncStoppedMsg{err: err})
	}
	go func() {
		defer close(heard)
		for {
			select {
			case err := <-m.stop:
				stopOn(err)
				return
			case answer := <-m.askStop:
				// A group of tables has ended, and asks whether one of its streams met a
				// critical error. The watch alone reads the channel: its answer leaves no
				// moment where the error is neither waiting there nor known.
				select {
				case err := <-m.stop:
					answer <- err
					stopOn(err)
					return
				default:
					answer <- nil
				}
			case <-m.ctx.Done():
				return
			}
		}
	}()
	return heard
}

// stoppedMeanwhile says, for a group of tables that has ended without a failure, whether the
// sync was stopped while it ran. A stream acknowledges the row it was refused, and its table
// ends as if it had been written: the group after it must not start for that.
func (m *model) stoppedMeanwhile() tea.Msg {
	answer := make(chan error, 1)
	select {
	case m.askStop <- answer:
		if err := <-answer; err != nil {
			return syncStoppedMsg{err: err}
		}
		return nil
	case <-m.ctx.Done():
		// Stopped already: by a critical error the watch has heard, or by a key.
		return syncFailedMsg{err: m.ctx.Err()}
	}
}

// finish stops the tables still being synced and waits for them, then for the watch of the
// critical errors: what it heard is a failure of the sync, told to the program or not.
func (m *model) finish(heard <-chan error, logger *slog.Logger) {
	m.cancel()
	if !m.inFlight.end(m.endWait) {
		logger.Warn("the tables being synced did not stop in time")
	}
	if stopErr, ok := <-heard; ok {
		m.fail(stopErr)
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			// The tables being synced are stopped at the key, not once the program has closed.
			m.cancel()
			return m, tea.Quit
		}
	case syncStoppedMsg:
		// The stream goes on past the row that was refused: the sync is stopped here.
		m.fail(msg.err)
		m.cancel()
		return m, tea.Quit
	case syncFailedMsg:
		// Tables cut short when the sync was stopped did not fail: the stop is what ended it.
		if m.ctx.Err() == nil || !errors.Is(msg.err, context.Canceled) {
			m.fail(msg.err)
		}
		return m, tea.Quit
	case syncedDataMsg:
		successStrs := []string{}
		for _, msgStr := range msg {
			successStrs = append(successStrs, msgStr)
			m.tableSynced++
		}
		if m.totalConfigCount == m.tableSynced {
			m.done = true
			m.logger.Info(fmt.Sprintf("Done! Completed %d tables.", m.tableSynced))
			return m, tea.Sequence(
				tea.Println(strings.Join(successStrs, " \n")),
				tea.Quit,
			)
		}

		m.index++
		return m, tea.Batch(
			tea.Println(strings.Join(successStrs, " \n")),
			m.syncConfigs(m.ctx, m.groupedConfigs[m.index]),
		)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) View() tea.View {
	configCount := getConfigCount(m.groupedConfigs)
	w := lipgloss.Width(fmt.Sprintf("%d", configCount))

	if m.done {
		return tea.NewView(doneStyle.Render(fmt.Sprintf("Done! Completed %d tables.\n", configCount)))
	}

	pkgCount := fmt.Sprintf(" %*d/%*d", w, m.tableSynced, w, configCount)

	spin := m.spinner.View() + " "
	cellsAvail := maxInt(0, m.width-lipgloss.Width(spin+pkgCount))

	processingTables := []string{}
	for _, config := range m.groupedConfigs[m.index] {
		processingTables = append(processingTables, config.Name)
	}

	var pkgName string
	if len(processingTables) > 5 {
		pkgName = currentPkgNameStyle.Render(
			fmt.Sprintf(
				"%s \n + %d others...",
				strings.Join(processingTables[:5], "\n"),
				len(processingTables),
			),
		)
	} else {
		pkgName = currentPkgNameStyle.Render(strings.Join(processingTables, "\n"))
	}
	info := lipgloss.NewStyle().MaxWidth(cellsAvail).Render("Syncing " + pkgCount + " \n" + pkgName)
	return tea.NewView(printlog.Render("\n") + spin + info)
}

// errSyncInterrupted is returned for a sync quit before its last table.
var errSyncInterrupted = errors.New("the sync was interrupted before its last table")

type syncedDataMsg map[string]string

// syncFailedMsg says a table of the group failed: the sync ends there.
type syncFailedMsg struct{ err error }

// syncStoppedMsg says a stream met a critical error — a row its destination refuses for good.
type syncStoppedMsg struct{ err error }

// fail records what ended the sync before its last table: the first failure is the cause, and
// those that follow it are its consequences.
func (m *model) fail(err error) {
	if m.err == nil {
		m.err = err
	}
}

func (m *model) syncConfigs(
	ctx context.Context,
	configs []*benthosbuilder.BenthosConfigResponse,
) tea.Cmd {
	return func() tea.Msg {
		// The program may have ended since it asked for the group.
		if !m.inFlight.begin() {
			return nil
		}
		defer m.inFlight.done()
		messageMap := syncmap.Map{}
		errgrp, errctx := errgroup.WithContext(ctx)
		errgrp.SetLimit(5)
		for _, cfg := range configs {
			cfg := cfg
			errgrp.Go(func() error {
				start := time.Now()
				m.logger.Info(fmt.Sprintf("Syncing table %s", cfg.Name))
				err := syncData(errctx, m.benv, cfg, m.logger, m.outputType)
				if err != nil {
					// The table that failed is the one told: those the failure cut short, or
					// kept from starting, only say the sync ended.
					if !errors.Is(err, context.Canceled) {
						m.logger.Error(fmt.Sprintf("Error syncing table %s: %s", cfg.Name, err.Error()))
					}
					return err
				}
				duration := time.Since(start)
				messageMap.Store(cfg.Name, duration)
				m.logger.Info(
					fmt.Sprintf("Finished syncing table %s %s", cfg.Name, duration.String()),
				)
				return nil
			})
		}

		if err := errgrp.Wait(); err != nil {
			return syncFailedMsg{err: err}
		}
		if stopped := m.stoppedMeanwhile(); stopped != nil {
			return stopped
		}

		results := map[string]string{}
		messageMap.Range(func(key, value any) bool {
			d := value.(time.Duration)
			results[key.(string)] = fmt.Sprintf("%s %s %s", checkMark, key,
				durationStyle.Render(d.String()))
			return true
		})
		message := ""
		for _, config := range configs {
			message = fmt.Sprintf("%s, %s", message, config.Name)
		}
		return syncedDataMsg(results)
	}
}

func getConfigCount(groupedConfigs [][]*benthosbuilder.BenthosConfigResponse) int {
	count := 0
	for _, group := range groupedConfigs {
		for _, config := range group {
			if config != nil {
				count++
			}
		}
	}
	return count
}

func runSync(
	ctx context.Context,
	outputType output.OutputType,
	benv *service.Environment,
	groupedConfigs [][]*benthosbuilder.BenthosConfigResponse,
	stop <-chan error,
	logger *slog.Logger,
) error {
	var opts []tea.ProgramOption
	var synclogger = logger
	if outputType == output.PlainOutput {
		// Plain mode don't render the TUI
		opts = []tea.ProgramOption{tea.WithoutRenderer(), tea.WithInput(nil)}
	} else {
		fmt.Println(bold.Render(" \n Completed Tables")) //nolint:forbidigo
		// TUI mode, discard log output
		synclogger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	m := newModel(ctx, benv, groupedConfigs, synclogger, outputType)
	m.stop = stop
	return runSyncProgram(m, tea.NewProgram(m, opts...), logger)
}

// syncEndWait bounds the wait for the tables of a sync that has ended to stop.
const syncEndWait = 5 * time.Second

// runSyncProgram runs the program of a sync until it ends, and tells how it ended. However it
// ends — its last table, a table that failed, a key or a signal — the tables still being
// synced are stopped and waited for: once the sync has returned, no write is started. A write
// under way when its table was stopped may still complete.
func runSyncProgram(m *model, program *tea.Program, logger *slog.Logger) error {
	heard := m.watchStop(program.Send)
	_, err := program.Run()
	m.finish(heard, logger)
	if err != nil && !errors.Is(err, tea.ErrInterrupted) {
		logger.Error(fmt.Sprintf("Error syncing data: %v", err))
	}
	return m.outcome(err)
}

// outcome tells how the sync ended, from how its program did.
func (m *model) outcome(programErr error) error {
	// A critical error the program was not told of before it ended: the stream acknowledges
	// the row it was refused, and the last table may have ended as if it had been written.
	select {
	case stopErr := <-m.stop:
		m.fail(stopErr)
	default:
	}
	switch {
	case programErr != nil && !errors.Is(programErr, tea.ErrInterrupted):
		return fmt.Errorf("unable to finish syncing data: %w", programErr)
	case m.err != nil:
		return fmt.Errorf("unable to finish syncing data: %w", m.err)
	case !m.done:
		// Quit before its last table, the sync did not do what it was asked to, whatever way
		// it was quit: a key, a signal.
		return fmt.Errorf("unable to finish syncing data: %w", errSyncInterrupted)
	}
	return nil
}
