<div align="center">
  <img src="https://github.com/user-attachments/assets/d2cc1874-5afe-4ca1-b087-018456f13bad" alt="crolendar_icon" width="220" />

  <h1>Crolendar</h1>

  <p>
    <strong>A modern Terminal User Interface for visualizing and managing cron schedules.</strong>
  </p>

  <p>
    <img src="https://img.shields.io/badge/Made%20with-Bubble%20Tea-00ADD8?style=flat-square&logo=go" alt="Bubble Tea" />
    <img src="https://img.shields.io/badge/Language-Go-00ADD8?style=flat-square&logo=go" alt="Go" />
    <img src="https://img.shields.io/badge/Go-1.21+-blue?style=flat-square" alt="Go Version" />
    <img src="https://img.shields.io/badge/License-MIT-green?style=flat-square" alt="License" />
  </p>

  <p>
    <a href="#"><strong>Documentation</strong></a> · 
    <a href="https://github.com/yourusername/crolendar"><strong>Source Code</strong></a> · 
    <a href="https://github.com/yourusername/crolendar/issues"><strong>Report Bug</strong></a>
  </p>
</div>

<br>

<br>

## Why Crolendar?

Crolendar transforms the cryptic syntax of crontabs into a clear, interactive timeline. Designed for developers and sysadmins who live in the terminal and need to visualize automation at a glance.

- **Visual Timeline:** Stop guessing when your jobs run. View your raw crontab schedules mapped onto a familiar calendar grid.
- **Vim-Inspired Navigation:** Move through your schedule with lightning speed using standard `h/j/k/l` motions.
- **Auto-Discovery:** Automatically attempts to load your system crontab, or falls back to a local `crontab.txt` for easy testing.
- **Lightweight & Fast:** Built with Go and the Charm libraries, providing a flicker-free, responsive TUI experience.
- **Context Aware:** Easily switch focus between different views (Sidebar and Main Grid) to drill down into specific job details.

## Installation

Get started quickly by building from source. Ensure you have **Go 1.21+** installed.

```bash
# Clone the repository
git clone https://github.com/yourusername/crolendar.git

# Navigate to the directory
cd crolendar

# Install dependencies and run
go mod tidy
go run .
```

## Controls

Navigation is designed to be keyboard-centric and intuitive for terminal users.

| Component | Shortcut | Action |
| :--- | :--- | :--- |
| **Global** | `Tab` | Switch focus (Sidebar / Grid) |
| | `q / Ctrl+C` | Quit Application |
| **Navigation** | `h / j / k / l` | Move Selection (Left / Down / Up / Right) |
| | `Arrow Keys` | Move Selection |
| **View** | `Enter` | Select/Inspect Job |

## Architecture

Crolendar is built on a modern TUI stack:

- **Frontend:** [Bubble Tea](https://github.com/charmbracelet/bubbletea) for reactive TUI components.
- **Styling:** [Lipgloss](https://github.com/charmbracelet/lipgloss) for layout and terminal-based CSS.
- **Parsing:** Custom Go-based cron parser to handle standard crontab formats.

<p align="center">
&copy; CodeFXR. All rights reserved.
</p>
