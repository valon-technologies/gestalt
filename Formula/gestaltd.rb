# frozen_string_literal: true

require_relative "lib/private_strategy"

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.6"
  license "Proprietary"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/v0.0.1-alpha.6/gestaltd-macos-arm64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "8a2e67f2d3829d832b020affed4c42f83a4a30ad0e42b17e96229f3fc3840af6"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/v0.0.1-alpha.6/gestaltd-macos-x86_64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "babf6c30b3b4714ce3bd65ae79dcef71acca6357ef2bb000aa074aafd321adf9"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/v0.0.1-alpha.6/gestaltd-linux-arm64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "b676fa4924e7fe60d58f0482f2ebe0a918fb2c5ecebd0271320a7e2bd860e972"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/v0.0.1-alpha.6/gestaltd-linux-x86_64.tar.gz",
          using: GitHubPrivateRepositoryReleaseDownloadStrategy
      sha256 "ec62b12f05ad5c65b5d3b7ecdf274698e86daac5c5adf5ee68d622caece4165d"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
