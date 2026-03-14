# Pre-Commit Review Infrastructure

## Overview

5-pass pre-commit review gate with automatic fallback chain. Designed for reliability with multiple AI providers.

## Gate Order

| Gate | Tool | Model | Provider | Status |
|------|------|-------|----------|--------|
| 1 | Copilot | `gpt-5-mini` | GitHub OAuth | ✅ Primary |
| 2 | Aider | `MiniMax-M2.5` | MiniMax API | ✅ Primary |
| 3 | Gemini | `gemini-2.5-flash-lite` | Google OAuth | ✅ Primary |
| 4 | PR-Agent | LiteLLM models | LiteLLM proxy | ⚡ Fallback |
| 5 | CodeRabbit | - | CodeRabbit API | ⚡ Fallback |

**Rules:**
- Stop when 2+ gates pass
- Rate-limited/timeout triggers fallback to next gate
- All gates must be installed and configured

## Required Environment Variables

```bash
# Primary providers (set at least 2)
MINIMAX_API_KEY=sk-cp-xxx              # Gate 2 - https://www.minimaxi.com
GEMINI_API_KEY=AIzaSyxxx               # Gate 3 (API key mode) - https://aistudio.google.com
OPENROUTER_API_KEY=sk-or-v1-xxx        # Gate 2 fallback - https://openrouter.ai

# Fallback providers
LITELLM_MASTER_KEY=sk-xxx              # Gate 4 - Local LiteLLM proxy
LITELLM_URL=http://localhost:4000/v1   # Gate 4

# Gate 5 is authenticated via ~/.coderabbit/auth.json
```

## Gemini OAuth Account Switching

Supports multiple Gemini accounts with automatic switching on rate limit.

**Account Configuration:**
```bash
# Primary OAuth directory
~/.gemini/

# Alternate OAuth directories (for account switching)
~/.gcli-b-home/.gemini/           # Alternate account
~/.gcli-oci-noauth-home/.gemini/  # Another alternate

# Dedicated credential files
~/.gemini/oauth_creds.msg.goel.json
~/.gemini/oauth_creds.agussalahi.json
```

**Available Models:**
- `gemini-2.5-flash-lite` (default, fast)
- `gemini-2.5-flash`
- `gemini-2.0-flash-exp`

## Free Model Options

### OpenRouter Free Models (Gate 2 fallback)
```
openrouter/qwen/qwen3-coder:free
openrouter/mistralai/mistral-small-3.1-24b-instruct:free
openrouter/google/gemini-2.0-flash-exp:free
```

### Ollama Local Models (Completely Free)
```bash
# Install Ollama
curl -fsSL https://ollama.ai/install.sh | sh

# Pull model
ollama pull llama3

# Use in aider
CODERO_AIDER_MODEL=ollama/llama3
```

## File Structure

```
scripts/review/
├── two-pass-review.sh      # Orchestrator (runs all gates)
├── copilot-third-pass.sh   # Gate 1 - GitHub Copilot CLI
├── aider-first-pass.sh     # Gate 2 - Aider with MiniMax/OpenRouter
├── gemini-second-pass.sh   # Gate 3 - Gemini CLI with OAuth switching
├── pr-agent-second-pass.sh # Gate 4 - PR-Agent via LiteLLM
├── coderabbit-second-pass.sh # Gate 5 - CodeRabbit CLI
└── install-pre-commit.sh   # Install git hook

.git/hooks/
└── pre-commit              # Points to two-pass-review.sh
```

## Installation

```bash
# 1. Install dependencies
pip install --break-system-packages aider-chat
npm install -g @github/copilot-cli

# 2. Authenticate services
copilot auth login              # GitHub OAuth
gemini                          # Google OAuth (interactive)
coderabbit auth login           # CodeRabbit OAuth

# 3. Set environment variables
cp .env.example .env
# Edit .env with your API keys

# 4. Install pre-commit hook
bash scripts/review/install-pre-commit.sh

# 5. Test
echo "// test" >> src/file.js && git add src/file.js
bash scripts/review/two-pass-review.sh
git checkout -- src/file.js
```

## Configuration Variables

```bash
# Timeouts
CODERO_GATE_TIMEOUT=90              # Per-gate timeout (seconds)
CODERO_FIRST_PASS_TIMEOUT_SEC=90    # Gate 2 specific
CODERO_SECOND_PASS_TIMEOUT_SEC=45   # Gate 3 specific
CODERO_COPILOT_TIMEOUT_SEC=60       # Gate 1 specific
CODERO_CODERABBIT_TIMEOUT_SEC=120   # Gate 5 specific

# Models
CODERO_AIDER_MODEL=minimax/MiniMax-M2.5
CODERO_GEMINI_MODEL=gemini-2.5-flash-lite
CODERO_COPILOT_MODEL=gpt-5-mini

# Retries
CODERO_GEMINI_MAX_RETRIES=3
```

## Customization

### Change Default Model
```bash
# Use different Aider model
CODERO_AIDER_MODEL=openrouter/qwen/qwen3-coder:free

# Use different Gemini model
CODERO_GEMINI_MODEL=gemini-2.5-pro
```

### Skip Gates
```bash
# Skip specific gates by removing from orchestrator
# Edit two-pass-review.sh, modify the for loop
```

### Add New Gate
```bash
# Create scripts/review/my-gate.sh
# Add to two-pass-review.sh for loop
```

## Troubleshooting

### Gate 1 (Copilot) Issues
```bash
# Re-authenticate
copilot auth login

# Check model availability
copilot --help | grep model
```

### Gate 2 (Aider) Issues
```bash
# Test API key
curl -X POST https://api.minimax.chat/v1/chat/completions \
  -H "Authorization: Bearer $MINIMAX_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"MiniMax-M2.5","messages":[{"role":"user","content":"hi"}]}'

# Check aider installation
aider --version
```

### Gate 3 (Gemini) Rate Limited
```bash
# Check available accounts
cat ~/.gemini/google_accounts.json

# Switch account manually
jq '.active = "agussalahi551@gmail.com"' ~/.gemini/google_accounts.json > /tmp/accounts.json
mv /tmp/accounts.json ~/.gemini/google_accounts.json
```

### Gate 5 (CodeRabbit) Timeout
```bash
# Increase timeout
CODERO_CODERABBIT_TIMEOUT_SEC=300

# Check authentication
coderabbit auth status
```

## Logs

Review logs are stored in:
```
.codero/review-logs/
├── orchestrator-TIMESTAMP.log
├── copilot-third-pass-TIMESTAMP.log
├── aider-first-pass-TIMESTAMP.log
├── gemini-second-pass-TIMESTAMP.log
└── coderabbit-second-pass-TIMESTAMP.log
```

## Emergency Bypass

```bash
# Skip all gates (use sparingly)
git commit --no-verify -m "[EMERGENCY] message"
```

## Version

- Infrastructure: v1.0.0
- Last Updated: 2026-03-14
- Tested On: mathkit-v2, cacheflow