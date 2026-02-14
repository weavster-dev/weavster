//! Weavster CLI
//!
//! Developer tool for local development and project management.

use anyhow::Result;
use clap::{Parser, Subcommand};
use tracing_subscriber::{EnvFilter, layer::SubscriberExt, util::SubscriberInitExt};

mod commands;
mod local_db;

/// Weavster - Modern Enterprise Service Bus
#[derive(Parser)]
#[command(name = "weavster")]
#[command(author, version, about, long_about = None)]
struct Cli {
    /// Configuration file path
    #[arg(short, long, default_value = "weavster.yaml")]
    config: String,

    /// Enable verbose logging
    #[arg(short, long)]
    verbose: bool,

    #[command(subcommand)]
    command: Commands,
}

#[derive(Subcommand)]
enum Commands {
    /// Initialize a new Weavster project
    Init {
        /// Directory to initialize (defaults to current directory)
        #[arg(default_value = ".")]
        path: String,

        /// Project name (defaults to directory name)
        #[arg(short, long)]
        name: Option<String>,
    },

    /// Compile flows to WASM
    Compile {
        /// Compile a specific flow only
        #[arg(short, long)]
        flow: Option<String>,

        /// Output generated Rust code for debugging
        #[arg(long)]
        debug: bool,

        /// Force recompile (ignore cache)
        #[arg(long)]
        force: bool,

        /// Configuration profile to use (e.g., dev, prod)
        #[arg(short, long)]
        profile: Option<String>,
    },

    /// Package flows into OCI artifact
    Package {
        /// Sign the artifact with cosign
        #[arg(long)]
        sign: bool,

        /// Output path for the artifact
        #[arg(short, long)]
        output: Option<String>,

        /// Configuration profile to use (e.g., dev, prod)
        #[arg(short, long)]
        profile: Option<String>,
    },

    /// Run the Weavster runtime
    Run {
        /// Run a specific flow only
        #[arg(short, long)]
        flow: Option<String>,

        /// Process one message and exit
        #[arg(long)]
        once: bool,

        /// Configuration profile to use (e.g., dev, prod)
        #[arg(short, long)]
        profile: Option<String>,
    },

    /// Validate configuration without running
    Validate {
        /// Configuration profile to use (e.g., dev, prod)
        #[arg(short, long)]
        profile: Option<String>,
    },

    /// Show project status
    Status,

    /// Manage flows
    Flow {
        #[command(subcommand)]
        command: FlowCommands,
    },

    /// Manage connectors
    Connector {
        #[command(subcommand)]
        command: ConnectorCommands,
    },

    /// Run tests
    Test {
        /// Test file or pattern
        pattern: Option<String>,
    },
}

#[derive(Subcommand)]
enum FlowCommands {
    /// List all flows
    List,

    /// Show flow details
    Show {
        /// Flow name
        name: String,
    },

    /// Create a new flow
    New {
        /// Flow name
        name: String,
    },
}

#[derive(Subcommand)]
enum ConnectorCommands {
    /// List configured connectors
    List,

    /// Test connector connectivity
    Test {
        /// Connector name
        name: String,
    },
}

#[tokio::main]
async fn main() -> Result<()> {
    let cli = Cli::parse();

    // Initialize logging
    let filter = if cli.verbose {
        EnvFilter::new("debug")
    } else {
        EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info"))
    };

    tracing_subscriber::registry()
        .with(filter)
        .with(tracing_subscriber::fmt::layer())
        .init();

    match cli.command {
        Commands::Init { path, name } => {
            commands::init::run(&path, name.as_deref()).await?;
        }
        Commands::Compile {
            flow,
            debug,
            force,
            profile,
        } => {
            commands::compile::run(
                &cli.config,
                flow.as_deref(),
                debug,
                force,
                profile.as_deref(),
            )
            .await?;
        }
        Commands::Package {
            sign,
            output,
            profile,
        } => {
            commands::package::run(&cli.config, sign, output.as_deref(), profile.as_deref())
                .await?;
        }
        Commands::Run {
            flow,
            once,
            profile,
        } => {
            commands::run::run(&cli.config, flow.as_deref(), once, profile.as_deref()).await?;
        }
        Commands::Validate { profile } => {
            commands::validate::run(&cli.config, profile.as_deref()).await?;
        }
        Commands::Status => {
            commands::status::run(&cli.config).await?;
        }
        Commands::Flow { command } => match command {
            FlowCommands::List => {
                commands::flow::list(&cli.config).await?;
            }
            FlowCommands::Show { name } => {
                commands::flow::show(&cli.config, &name).await?;
            }
            FlowCommands::New { name } => {
                commands::flow::new(&cli.config, &name).await?;
            }
        },
        Commands::Connector { command } => match command {
            ConnectorCommands::List => {
                commands::connector::list(&cli.config).await?;
            }
            ConnectorCommands::Test { name } => {
                commands::connector::test(&cli.config, &name).await?;
            }
        },
        Commands::Test { pattern } => {
            commands::test::run(&cli.config, pattern.as_deref()).await?;
        }
    }

    Ok(())
}
