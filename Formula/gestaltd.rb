# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.32"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.32/gestaltd-macos-arm64.tar.gz"
      sha256 "a7c73c5fda41399405e411c965a68426abf4509d3556f014cd744d77e242f08e"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.32/gestaltd-macos-x86_64.tar.gz"
      sha256 "b3b9d1270208fc2f96c59e3b5452498493307fdda43bb861fa81b3f20ba1e365"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.32/gestaltd-linux-arm64.tar.gz"
      sha256 "b768029c81efe73003861ca6a328e0e403921db78dd8b51cb90a8d0b84df819e"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.32/gestaltd-linux-x86_64.tar.gz"
      sha256 "7d421f128a0ee54cbe82ee24b82670006a15a10f81ffa43cc081e2df390cee22"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
