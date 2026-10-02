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
	// err is what ended the sync before its last table.
	err error
	// inFlight counts the groups of tables being synced: a sync that ends waits for them.
	inFlight syncmap.WaitGroup
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
	return &model{
		ctx:              ctx,
		groupedConfigs:   groupedConfigs,
		tableSynced:      0,
		spinner:          s,
		totalConfigCount: getConfigCount(groupedConfigs),
		logger:           logger,
		outputType:       outputType,
		benv:             benv,
	}
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.syncConfigs(m.ctx, m.groupedConfigs[m.index]), m.spinner.Tick)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			// Quit before its last table, the sync did not do what it was asked to.
			if !m.done && m.err == nil {
				m.err = errSyncInterrupted
			}
			return m, tea.Quit
		}
	case syncFailedMsg:
		m.err = msg.err
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

func (m *model) syncConfigs(
	ctx context.Context,
	configs []*benthosbuilder.BenthosConfigResponse,
) tea.Cmd {
	// Counted here, where the program asks for the group, and not once it runs: the program
	// may end in between.
	m.inFlight.Add(1)
	return func() tea.Msg {
		defer m.inFlight.Done()
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
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := newModel(ctx, benv, groupedConfigs, synclogger, outputType)
	return runSyncProgram(m, cancel, tea.NewProgram(m, opts...), logger)
}

// interruptedSyncWait bounds the wait for the tables of a sync that has ended to stop.
const interruptedSyncWait = 5 * time.Second

// runSyncProgram runs the program of a sync until it ends, and tells how it ended. However it
// ends — its last table, a table that failed, a key or a signal — the tables still being
// synced are stopped, and waited for: the sync writes nothing once it has returned.
func runSyncProgram(m *model, cancel context.CancelFunc, program *tea.Program, logger *slog.Logger) error {
	final, err := program.Run()
	cancel()
	if !waitFor(&m.inFlight, interruptedSyncWait) {
		logger.Warn("the tables being synced did not stop in time")
	}
	if err != nil {
		logger.Error(fmt.Sprintf("Error syncing data: %v", err))
		return fmt.Errorf("unable to finish syncing data: %w", err)
	}
	// A table that failed, or a key that quit the sync, ends the program without an error of
	// its own: the model carries it.
	if m, ok := final.(*model); ok && m.err != nil {
		return fmt.Errorf("unable to finish syncing data: %w", m.err)
	}
	return nil
}

// waitFor waits for the group, for a time: it says whether the group was done by then.
func waitFor(group *syncmap.WaitGroup, timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		defer close(done)
		group.Wait()
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}
