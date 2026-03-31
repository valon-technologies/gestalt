use clap_markdown::MarkdownOptions;
use gestalt::cli::Cli;

fn main() {
    let options = MarkdownOptions::new()
        .title("gestalt CLI Reference".to_string())
        .show_footer(false)
        .show_table_of_contents(true);

    let markdown = clap_markdown::help_markdown_custom::<Cli>(&options);
    print!("{markdown}");
}
