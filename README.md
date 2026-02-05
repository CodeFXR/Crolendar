<div align="center">
# Crolendar

<img width="400" height="404" alt="crolendar" src="https://github.com/user-attachments/assets/d2cc1874-5afe-4ca1-b087-018456f13bad" />
<p>

**Crolendar** is a modern Terminal User Interface (TUI) application designed to visualize and manage your cron jobs in a familiar calendar layout. Built with Go, Bubble Tea, and Lipgloss, it transforms raw crontab schedules into an interactive, readable timeline.

## Quick Start

### Prerequisites
- Go 1.21+

### Installation

Clone the repository and run the application:

```bash
git clone https://github.com/yourusername/crolendar.git
cd crolendar
go mod tidy
go run .
```

By default, Crolendar looks for a `crontab.txt` file in the current directory. If not found, it attempts to load the system crontab.

## Usage

Navigation is keyboard-centric, inspired by Vim motions.

- **Tab**: Switch focus between the **Sidebar** (View Selection) and the **Main Grid** (Calendar/Settings).
- **h / j / k / l** (or Arrow Keys): Navigate.
- **q**: Quit the application.
