package main

//run go mod tidy to import the bubble tea package
import (
	"fmt"
	"log"           //for printing errors etc
	"os"            //for workign with files in your local OS
	"path/filepath" //for saving notes in the required filepath
	"strings"       //all string functions like join etc

	textarea "charm.land/bubbles/v2/textarea"   //imports textarea for text input
	textinput "charm.land/bubbles/v2/textinput" //imports bubbles package for handling text input
	tea "charm.land/bubbletea/v2"               //importing bubble tea as tea
	gloss "charm.land/lipgloss/v2"              //the styling colors etc, goes in view for rendering purpose
)

var ( //global variables, will add as we go
	vaultdir string //uses go's init to find home directory

	activeTabStyle = gloss.NewStyle(). //active tabs
			Bold(true).
			Foreground(gloss.Color("#ffffff")).
			Background(gloss.Color("#6843ac")).
			Padding(0, 2)

	tabStyle = gloss.NewStyle(). //inactive tabs
			Bold(true).
			Foreground(gloss.Color("#77a5ca")).
			Background(gloss.Color("#1f0885")).
			Padding(0, 2)

	tabsRowStyle = gloss.NewStyle(). //tab border below the tabs
			Border(gloss.NormalBorder(), false, false, true, false).
			BorderForeground(gloss.Color("#d394fd")).
			PaddingBottom(1)
)

// model stores the app's CURRENT state
type model struct { //one note
	newfileinput   textinput.Model
	newfilenames   []string //list of filenames created with ctrl+N
	textVisibility bool
	noteFiles      []*os.File  //mabye ill refactor this to just an array of filenames had the wrong idea about file pointers, but they do work
	notetextareas  []textarea.Model
	activeTab      int //stores info for currently active tab
	viewMode       bool
	savedFiles     []string
}

// now we define what its INITIAL state is : Init function
func initialModel() model { //function that takes nothing and returns a struct

	// Create the vault directory if it doesn't exist to store the notes
	err := os.MkdirAll(vaultdir, 0750) //0750 is about file permissions
	if err != nil {
		log.Fatal(err)
	}

	// Creating the text input model
	input := textinput.New()
	input.Placeholder = "new filename (dont include .md)"
	input.Focus()
	input.CharLimit = 80
	input.SetWidth(80) // Updated to use the supported textinput width setter

	//the return statment
	return model{
		newfileinput:   input,
		textVisibility: false,
		//rest of the struct members will have default values
	}
}

func init() { //the GO init to get their home directory
	homedir, err := os.UserHomeDir()

	if err != nil {
		log.Fatal(err)
	}

	vaultdir = filepath.Join(homedir, ".cmdnote")
}

// init used for anything to be done before the app starts running (the bubble tea init)
func (m model) Init() tea.Cmd { //takes a model returns a cmd that can do io
	// Just return `nil`, which means "no I/O right now, please."
	return nil
}

// the update function that takes the keypress that has happened and passes it through switch to see what to do next
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) { //returns new model state or cmd

	var cmds []tea.Cmd //an array of cmds
	var cmd tea.Cmd

	switch msg := msg.(type) { //switches the TYPE of msg

	case tea.MouseClickMsg: //handles mouse click events, rn uses only X axis to decide for now
		m2 := msg.Mouse()
		if m2.Button == tea.MouseLeft {
			x := m2.X
			cursor := 0
			for i, name := range m.newfilenames {
				var w int
				if i == m.activeTab {
					w = gloss.Width(activeTabStyle.Render(name))
				} else {
					w = gloss.Width(tabStyle.Render(name))
				}
				if x >= cursor && x < cursor+w {
					m.activeTab = i                //mabye handling more cases will be more convienient later
					if i < len(m.noteFiles) && m.noteFiles[i] != nil {
						content, err := os.ReadFile(m.noteFiles[i].Name())
						if err == nil {
							m.notetextareas[i].SetValue(string(content))
						}
					}
					return m, nil
				}
				cursor += w
			}
		}

	case tea.KeyMsg:

		switch msg.String() {

		case "ctrl+c", "q":
			return m, tea.Quit

		case "ctrl+n": //to open a new tab
			m.textVisibility = true //opens the input area for it to take the new filename
			m.viewMode = false
			m.newfileinput.Placeholder = "new filename (dont include .md)"
			m.newfileinput.SetValue("")
			return m, nil

		case "ctrl+o":
			entries, err := os.ReadDir(vaultdir)
			if err != nil {
				return m, nil
			}
			m.savedFiles = []string{}
			for _, tempfile := range entries {
				if !tempfile.IsDir() && strings.HasSuffix(tempfile.Name(), ".md") {
					m.savedFiles = append(m.savedFiles, strings.TrimSuffix(tempfile.Name(), ".md"))
				}
			}
			m.viewMode = true
			m.textVisibility = false
			m.newfileinput.Placeholder = "enter file number to open respective file"
			m.newfileinput.SetValue("")
			return m, nil

		case "escape":
			m.viewMode = false
			m.textVisibility = false
			m.newfileinput.Placeholder = "new filename (dont include .md)"
			m.newfileinput.SetValue("")
			return m, nil

		case "ctrl+h", "left": // Navigate left
			if m.activeTab > 0 {
				m.activeTab--
				if m.activeTab < len(m.noteFiles) && m.noteFiles[m.activeTab] != nil {
					content, err := os.ReadFile(m.noteFiles[m.activeTab].Name())
					if err == nil {
						m.notetextareas[m.activeTab].SetValue(string(content))
					}
				}
			}
			return m, nil

		case "ctrl+l", "right": // Navigate right
			if m.activeTab < len(m.notetextareas)-1 {
				m.activeTab++
				if m.activeTab < len(m.noteFiles) && m.noteFiles[m.activeTab] != nil {
					content, err := os.ReadFile(m.noteFiles[m.activeTab].Name())
					if err == nil {
						m.notetextareas[m.activeTab].SetValue(string(content))
					}
				}
			}
			return m, nil

		case "ctrl+s":
			//saving data
			if len(m.noteFiles) == 0 || m.noteFiles[m.activeTab] == nil {
				break
			}
			f := m.noteFiles[m.activeTab]
			if err := f.Truncate(0); err != nil {
				fmt.Printf("Error saving file: %v\n", err)
				return m, nil
			}
			if _, err := f.Seek(0, 0); err != nil {
				fmt.Printf("Error saving file: %v\n", err)
				return m, nil
			}
			if _, err := f.WriteString(m.notetextareas[m.activeTab].Value()); err != nil {
				fmt.Printf("Error saving file: %v\n", err)
				return m, nil
			}
			if err := f.Sync(); err != nil {
				fmt.Printf("Error syncing file to disk: %v\n", err)
			}
			return m, nil

		case "enter":

			if m.viewMode {
				idxStr := m.newfileinput.Value()
				if idxStr == "" {
					return m, nil
				}
				idx := 0
				fmt.Sscanf(idxStr, "%d", &idx)
				idx--
				m.newfileinput.SetValue("")
				if idx < 0 || idx >= len(m.savedFiles) {
					return m, nil
				}
				name := m.savedFiles[idx]
				for _, existing := range m.newfilenames {
					if existing == name {
						m.viewMode = false
						return m, nil
					}
				}
				fpath := filepath.Join(vaultdir, name+".md")
				content, err := os.ReadFile(fpath)
				if err != nil {
					return m, nil
				}
				f, err := os.OpenFile(fpath, os.O_RDWR, 0644)
				if err != nil {
					return m, nil
				}
				m.newfilenames = append(m.newfilenames, name)
				m.noteFiles = append(m.noteFiles, f)

				textArea := textarea.New() //opens new text area for that specific file
				textArea.Placeholder = "Write your notes here..."
				textArea.SetValue(string(content))
				textArea.Focus()
				m.notetextareas = append(m.notetextareas, textArea) //adding new text area to array of textareas
				m.activeTab = len(m.notetextareas) - 1              //active tab becomes the latest added tab
				m.viewMode = false
				m.newfileinput.Placeholder = "new filename (dont include .md)"
				return m, nil
			}

			filename := m.newfileinput.Value()

			if filename != "" {
				fpath := filepath.Join(vaultdir, filename+".md")
				if _, err := os.Stat(fpath); err == nil {
					return m, nil
				}
				f, err := os.Create(fpath) //creates the file
				if err != nil {
					log.Fatalf("%v", err)
				}

				m.textVisibility = false                          //closes the filename input box
				m.newfilenames = append(m.newfilenames, filename) //adds it to the array of filenames in model
				m.noteFiles = append(m.noteFiles, f)
				m.newfileinput.SetValue("") //sets the filename inputbox back to null

				textArea := textarea.New() //opens new text area for that specific file
				textArea.Placeholder = "Write your notes here..."
				textArea.SetValue("")
				textArea.Focus()

				m.notetextareas = append(m.notetextareas, textArea) //adding new text area to array of textareas
				m.activeTab = len(m.notetextareas) - 1              //active tab becomes the latest added tab
				return m, nil
			}
		}
	}

	if m.textVisibility || m.viewMode {
		m.newfileinput, cmd = m.newfileinput.Update(msg)
	}
	if len(m.noteFiles) > 0 && m.activeTab < len(m.noteFiles) && m.noteFiles[m.activeTab] != nil { //i.e. if there's a file selected
		m.notetextareas[m.activeTab], cmd = m.notetextareas[m.activeTab].Update(msg)
		cmds = append(cmds, cmd)
	}

	// Return the updated model to the Bubble Tea runtime for processing. This is the case of no keystroke
	// Note that we're not returning a command.
	return m, cmd
}

func (m model) View() tea.View { //whatever is model, it returns a tea.View i.e. the UI we need

	var style = gloss.NewStyle(). //style for the header
					Bold(true).
					Foreground(gloss.Color("#FAFAFA")).
					Background(gloss.Color("#7D56F4")).
					PaddingLeft(4). //for extending the text block a little more
					PaddingRight(4)

	welc := style.Render("Welcome to BubbleTea Cafe! 🧋") //the very first title of the app

	help := "ctrl+N : new file | ctrl+O : view files | arrows/click : change tab | ctrl+S : save | ctrl+C/q : quit" //line that displays available commands

	view := "" //main UI

	if m.viewMode {
		if len(m.savedFiles) == 0 {
			view = "No saved notes found.\n\nPress ESC to go back."
		} else {
			lines := []string{"Your saved notes (type number + enter to open):\n"}
			for i, name := range m.savedFiles {
				lines = append(lines, fmt.Sprintf("  %d. %s", i+1, name))
			}
			lines = append(lines, "\n"+m.newfileinput.View())
			lines = append(lines, "Press ESC to go back.")
			view = strings.Join(lines, "\n")
		}
	} else if m.textVisibility {
		view = m.newfileinput.View()
	} else if len(m.noteFiles) > 0 && m.activeTab < len(m.noteFiles) && m.noteFiles[m.activeTab] != nil { //its either filename or file so added elseif
		view = m.notetextareas[m.activeTab].View()
	}

	//rendering how the tabs look
	var tabs []string
	for i := range m.newfilenames {
		title := m.newfilenames[i]
		if i == m.activeTab {
			tabs = append(tabs, activeTabStyle.Render(title))
		} else {
			tabs = append(tabs, tabStyle.Render(title))
		}
	}
	renderedTabs := tabsRowStyle.Render(strings.Join(tabs, "")) //joining all rendered tabs together

	s := fmt.Sprintf("\n%s\n\n%s\n\n%s\n\n%s", welc, renderedTabs, view, help) //returns the fmt specified string

	v := tea.NewView(s)
	v.MouseMode = tea.MouseModeCellMotion //enables mouse support via bubbletea v2 view-level mouse mode
	return v
}

// finally running the main thing, quits it if theres an error or q is pressed
func main() {
	p := tea.NewProgram(initialModel())
	if _, err := p.Run(); err != nil { //run New Program, if there IS an error print not running
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}