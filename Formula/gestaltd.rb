# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.9"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.9/gestaltd-macos-arm64.tar.gz"
      sha256 "c4a5f7cfa2c3b5d54151a0bd9b96f5ead6905fe62468c9c1dbe31317558279e6"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.9/gestaltd-macos-x86_64.tar.gz"
      sha256 "ded925a36cede25ae0a92405d044cd02213a70a8a0c929a3ae584482ad5847d0"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.9/gestaltd-linux-arm64.tar.gz"
      sha256 "0d423e65e3d0dfd713e419c8126b616c68826ce8ac098c804d0352eaba1cbe55"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.9/gestaltd-linux-x86_64.tar.gz"
      sha256 "e53e696d5b98e0fc027d835962ca696924b86b926a70c1c0e1d11b9b6bd98a86"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
