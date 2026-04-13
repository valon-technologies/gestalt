# frozen_string_literal: true

require_relative "lib/private_strategy"

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.13"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.13/gestaltd-macos-arm64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "5fcfafd41790e4c11d6fb079a5d2c8dac7fb7ef4aff75cf137df30c69febd943"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.13/gestaltd-macos-x86_64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "83139454feec21c6d994d692d4bc001927317f1de270a95a0c451ded2a27690f"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.13/gestaltd-linux-arm64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "3341534610cd56a25af44bce96c11001a614f162dfd605a1ace95d2f57e026c8"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.13/gestaltd-linux-x86_64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "2cac13af90412313be8c185019a43697a8f00b95e275c918112928242aebb167"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
