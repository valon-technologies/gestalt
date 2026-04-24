# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.4"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.4/gestaltd-macos-arm64.tar.gz"
      sha256 "8642ac2a452bcaf58eba3bd1be9a5aa4ab003ef0706a09df28b0c74398b7513a"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.4/gestaltd-macos-x86_64.tar.gz"
      sha256 "217ad51c1c438f75593dcd87ae841aa83590f3d1d40e1af73077e594cef30f17"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.4/gestaltd-linux-arm64.tar.gz"
      sha256 "c8dfb2dc156ca0def0e4c8a97c14eae7e42fcabdc4cdb8b58d890201e501f7ea"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.4/gestaltd-linux-x86_64.tar.gz"
      sha256 "ae870d6133f56b07cd5afab588e07c41cb2f72a373c1f7c9ba7a5c4acee9242e"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
